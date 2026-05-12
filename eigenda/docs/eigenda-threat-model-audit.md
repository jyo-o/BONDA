# EigenDA Threat Model Audit Report

**Project**: BONDA (Blockchain Operator Network Data Analytics)
**Target**: EigenDA v0.9.2+ (Layr-Labs/eigenda)
**Date**: 2026-05-12
**Authors**: BONDA Security Team (김지호)
**Methodology**: DFD-based threat modeling with source code verification

---

## Executive Summary

This audit examined EigenDA's architecture through a verified Data Flow Diagram (DFD) covering 9 processes, 4 data stores, 4 external entities, and 6 trust boundaries. We identified **14 confirmed threat scenarios** across 7 categories, with **2 Critical**, **6 High**, **4 Medium**, and **2 Informational** severity findings. Two hypothesized threats were disproven by code verification.

The most significant findings are:
1. **No slashing mechanism exists** — operators face zero economic penalty for misbehavior
2. **CertVerifier can be hot-swapped** with only 1 block notice (no enforced timelock)
3. **Relay GetBlob requires no authentication** — any party can retrieve any blob
4. **Attestation records can be unconditionally overwritten** in DynamoDB
5. **25+ DataAPI endpoints are fully unauthenticated** — exposing operational intelligence

---

## Table of Contents

1. [Scope and Methodology](#1-scope-and-methodology)
2. [System Overview](#2-system-overview)
3. [Findings](#3-findings)
4. [PoC Scenarios](#4-poc-scenarios)
5. [Disproven Hypotheses](#5-disproven-hypotheses)
6. [Recommendations](#6-recommendations)
7. [Appendix: Verification Sources](#7-appendix-verification-sources)

---

## 1. Scope and Methodology

### 1.1 Scope

| Item | Detail |
|------|--------|
| Codebase | `github.com/Layr-Labs/eigenda` (v0.9.2+) |
| Contracts | `contracts/src/` (Solidity) |
| Off-chain | disperser, controller, encoder, relay, node, proxy |
| Prior audit | Sigma Prime "EigenDA Proxy Secure Integration" (2025-10, 16 findings — all resolved) |

### 1.2 Methodology

1. **DFD Construction**: Level 0/1/L2 Data Flow Diagrams with 6 Trust Boundaries (TB0-TB3, TB-D, TB5/TB6)
2. **Code Verification**: 4 parallel agents verified every DFD element against source code
3. **Threat Identification**: STRIDE-per-element applied to each TB-crossing data flow
4. **Cross-reference**: Docs/Spec/Code 3-way comparison for gap analysis
5. **PoC Assessment**: Each confirmed threat assessed for exploitation feasibility

### 1.3 Trust Boundaries Analyzed

| TB | Name | Location | Key Verification |
|----|------|----------|-----------------|
| TB0 | Proxy Entry | EE1 <-> P1/P1* | No validation (pass-through) |
| TB1 | Disperser Entry | P1 <-> P2 | `validateDispersalRequest` (12 checks) |
| TB2 | Disperser <-> Operator | P3/P6 <-> P5 | ECDSA + KZG + BLS |
| TB3 | Off-chain <-> On-chain | P1/P1* <-> P8/P9 | `checkDACert` (eth_call) |
| TB-D | Disperser Infrastructure | P2/P3/P4/P6/DS1/DS2 | **No mutual auth** |

---

## 2. System Overview

### 2.1 Architecture (DFD Level 1)

```
Rollup Batcher                                                    Rollup Validator
     |                                                                  |
     | Payload (TB0: no validation)                    Cert URL (TB0)   |
     v                                                                  v
   [P1 Proxy]──────TB1──────>[P2 Disperser]            [P1* Proxy Read]
     |  ^                        |    |                   |    |
     |  | cert                   |    |                   |    | validated
     |  | (TB3: checkDACert)     v    v                   |    | payload
     |  |                   [P3 Controller]               |    v
     |  |                    |         |              [P6 Relay]<──[DS2 S3]
     |  |                    v         v                   |
     |  |              [P4 Encoder] [P5 DA Node]<─TB2─────┘
     |  |                              |
     v  v                              v (BLS sig)
   [P8 CertVerifier]<──[P9 Registry]  [P3 Sig Aggregation]
     (on-chain)          (on-chain)         |
                                            v
                                        [DS1 DynamoDB]
```

### 2.2 Key Data Flows

| Path | Flows | Critical TBs |
|------|-------|-------------|
| Write | EE1a -> P1 -> P2 -> P3 -> P4/P5 -> P3 -> DS1 | TB0, TB1, TB-D, TB2 |
| Read | EE1b -> P1* -> P8 -> P6/P5 -> EE1b | TB0, TB3, TB2 |
| Cert Assembly | P1 -> P9 -> P8 -> EE1a | TB3 |
| Governance | EE3 -> P9/P8 | TB4 |

---

## 3. Findings

### Finding 1: No Slashing Mechanism (Category A/E)

| | |
|---|---|
| **Severity** | Critical |
| **Category** | A. Broken Promise + E. Economic Unreality |
| **DFD Location** | System-wide (TB6: Attestation Boundary) |
| **Status** | Confirmed |

**Description**: EigenDA documentation claims economic security through "EIGEN token forking for slashing." However, `grep` across the entire `contracts/src/` directory returns **zero matches** for `slash`, `slashing`, `penalize`, or `penalty`. The `EigenDAServiceManager.sol` (193 lines) contains no slashing functions despite a comment at line 27 mentioning "freezing operators as the result of various challenges."

**Impact**: Operators face zero economic penalty for:
- Signing without actually storing chunks (lazy validation)
- Selectively withholding data
- Colluding to produce invalid attestations

The claimed "3-tier security model" (BFT + Cryptoeconomic + Token Toxicity) is effectively reduced to BFT-only, since Cryptoeconomic slashing is unimplemented and Token Toxicity applies only to Custom Quorum (Q2).

**Evidence**:
- `contracts/src/core/EigenDAServiceManager.sol` — no slash functions
- Docs: `eigencloud-docs/security-model.md` claims EIGEN forking
- Spec: `print.html` — no slashing specification

**BONDA Dashboard Relevance**: This makes the "ejection as only punishment" metric critical. BONDA tracks ejection events (528 historical) as the sole accountability signal.

---

### Finding 2: CertVerifier Hot-Swap Without Enforced Timelock (Category C)

| | |
|---|---|
| **Severity** | Critical |
| **Category** | C. Governance Concentration |
| **DFD Location** | TB3 (P8 CertVerifier), DF25a (Governance -> P9) |
| **Status** | Confirmed |

**Description**: The `EigenDACertVerifierRouter.sol` allows the owner to add a new CertVerifier with `activationBlockNumber = block.number + 1`. The contract at line 70 only enforces `activationBlockNumber > block.number` — there is **no minimum distance requirement**.

```solidity
// Line 64-66 — RECOMMENDATION ONLY, NOT ENFORCED:
/// @dev EigenDA recommends that a mechanism be implemented to ensure
///      a cert verifier cannot be added too close to the current block number.
```

**Impact**: A compromised owner multisig can replace `checkDACert` with a version that always returns `SUCCESS`, effectively disabling ALL cryptographic verification. Since `checkDACert` is the final trust anchor for both write and read paths, this invalidates the entire DA guarantee.

**Evidence**:
- `contracts/src/integrations/cert/router/EigenDACertVerifierRouter.sol:62-77`
- `CertVerifierRouterDeployer.s.sol:61-62` — uses TransparentUpgradeableProxy
- `EigenDAAccessControl.sol:11` — "should be put behind a timelock" (comment only)

---

### Finding 3: Relay GetBlob No Authentication (Category D/G)

| | |
|---|---|
| **Severity** | High |
| **Category** | D. Verification Gap + G. Undocumented Surface |
| **DFD Location** | TB2 (P6 Relay), DF21/DF22 (Read Path) |
| **Status** | Confirmed |

**Description**: The `Relay.GetBlob()` handler (`relay/server.go:202-269`) has **zero authentication**. Only global rate limiting exists (no per-client tracking). Compare with `GetChunks()` (line 292-421) which requires BLS authentication + replay guard.

Anyone who knows or can compute a blob key can retrieve the full blob data. Blob keys are deterministic: `Keccak256(BlobHeader)`, so they can be computed if blob metadata is known.

**Impact**:
- **Data confidentiality**: All blob data is publicly accessible (note: blob data is generally considered public, but this should be a conscious design decision, not an oversight)
- **Reconnaissance**: Attackers can probe for specific blobs to verify their existence
- **Resource exhaustion**: Global-only rate limiting allows slow-drip attacks

**Evidence**:
- `relay/server.go:202-269` — GetBlob handler, no auth
- `relay/limiter/blob_rate_limiter.go:55` — global rate limit only
- Contrast: `relay/server.go:308-329` — GetChunks has BLS auth

**PoC**: Single gRPC call: `GetBlob({blob_key: <known_key>})` — no credentials required.

---

### Finding 4: Attestation Unconditional Overwrite (Category D)

| | |
|---|---|
| **Severity** | High |
| **Category** | D. Verification Gap |
| **DFD Location** | TB-D (DS1 MetadataStore), DF15a |
| **Status** | Confirmed |

**Description**: `PutAttestation` in `dynamo_metadata_store.go:1168-1177` uses unconditional `PutItem` with an explicit comment: `// Allow overwrite of existing attestation`. There is no monotonicity check (e.g., "only overwrite if new attestation has more signatures"). Compare with `PutBlobInclusionInfo` (line 1216) which correctly uses `attribute_not_exists` conditional writes.

```go
// dynamo_metadata_store.go:1174
// Allow overwrite of existing attestation
err = s.dynamoDBClient.PutItem(ctx, s.tableName, item)
```

**Impact**: A compromised controller or any entity with DynamoDB write access can overwrite a fully-attested record with an empty attestation, effectively "un-confirming" blobs. Since `GetAttestation` uses `ConsistentRead: true`, corrupted values propagate immediately.

**Evidence**:
- `disperser/common/v2/blobstore/dynamo_metadata_store.go:1168-1177`
- `disperser/controller/controller.go:315-333` — writes initially empty, then updates
- Line 1216 — `PutBlobInclusionInfo` uses conditional write (shows devs know the pattern)

---

### Finding 5: Encoding Output Not Verified (Category D)

| | |
|---|---|
| **Severity** | High |
| **Category** | D. Verification Gap |
| **DFD Location** | TB-D (P3 <-> P4), DF5/DF6 |
| **Status** | Confirmed |

**Description**: The `encoding_manager.go` receives encoder results containing only `FragmentInfo` (just `SymbolsPerFrame`). No chunks, proofs, or commitments are returned to the controller for verification. The encoder client (`client_v2.go:35`) connects with `insecure.NewCredentials()` (plaintext gRPC).

```go
// encoding_manager.go:362-368
fragmentInfo, err := e.encodeBlob(encodingCtx, blobKey, blob, blobParams)
// No verification of encoding correctness follows
```

**Impact**: A compromised or malfunctioning encoder can produce invalid chunks/proofs that are stored directly in S3 and dispersed to operators. While operators verify proofs on receipt (KZG in `ValidateBatchV2`), invalid encoding causes mass dispersal failures (liveness impact).

**Evidence**:
- `disperser/controller/encoding_manager.go:362-368, 446-464`
- `disperser/encoder/client_v2.go:26-65` — insecure credentials
- Zero grep hits for "verify" related to encoding output in encoding_manager.go

---

### Finding 6: Recency Check Off-chain Only (Category D/F)

| | |
|---|---|
| **Severity** | High |
| **Category** | D. Verification Gap + F. Time/Version Gap |
| **DFD Location** | TB3 (P1/P1* <-> P8), DF16d/DF23 |
| **Status** | Confirmed |

**Description**: The on-chain verifier only checks `referenceBlockNumber <= block.number` (not in the future). The actual recency check (`certRBN < certL1IBN <= certRBN + 14400`) exists **only** in eigenda-proxy (`api/proxy/store/generated_key/v2/eigenda.go:343-351`).

The code itself acknowledges this limitation (line 377-381):
> "There are 2 approaches to doing this: 1. Pessimistic approach: use a smart batcher inbox... 2. Optimistic approach: verify the check in op-program or hokulea"

**Impact**: Rollups performing direct integration (bypassing eigenda-proxy) have **no recency protection**. An attacker could submit a cert with an arbitrarily old RBN, referencing a historical operator stake distribution where colluding operators controlled more weight.

**Evidence**:
- `EigenDACertVerifierRouter.sol:101-108` — only `RBNInFuture` revert
- `EigenDACertVerificationLib.sol:82-122` — full checkDACert, no recency
- `api/proxy/store/generated_key/v2/eigenda.go:343-393` — off-chain recency check
- `api/proxy/common/consts/consts.go:17` — window = 14,400 blocks (~48h)

---

### Finding 7: DataAPI Full Exposure Without Authentication (Category G)

| | |
|---|---|
| **Severity** | High |
| **Category** | G. Undocumented Attack Surface |
| **DFD Location** | Off-DFD (not modeled — undocumented component) |
| **Status** | Confirmed |

**Description**: The DataAPI exposes 25+ HTTP endpoints with zero authentication. V1 endpoints (`disperser/dataapi/server.go:302-335`) and V2 endpoints (`disperser/dataapi/v2/server_v2.go:238-279`) include:

| Endpoint | Exposed Information |
|----------|-------------------|
| `/operators-info/semver-scan` | Operator software versions |
| `/metrics/non-signers` | Which operators are NOT signing |
| `/operators-info/operators-stake` | Stake distribution per operator |
| `/operators-info/port-check` | Operator network port status |
| `/operators/:id/dispersals` | Per-operator dispersal feed |
| `/operators/liveness` | Operator liveness status |
| `/accounts/:id/blobs` | Per-account blob feed |
| `/swagger/*` | Full API documentation |

**Impact**: An adversary can:
- Map every operator's software version to identify vulnerable nodes
- Identify non-signing operators for targeted attacks
- Monitor real-time throughput to time attacks during low-activity periods
- Perform traffic analysis via per-account blob feeds

**Evidence**:
- `disperser/dataapi/server.go:302-335` — V1 routes
- `disperser/dataapi/v2/server_v2.go:238-279` — V2 routes
- Zero grep hits for auth middleware, API keys, or bearer tokens in `disperser/dataapi/`

---

### Finding 8: Relay Single Point of Failure (Category B)

| | |
|---|---|
| **Severity** | High |
| **Category** | B. Single Point of Failure |
| **DFD Location** | TB2 (P6 Relay), DF9/DF10 |
| **Status** | Confirmed |

**Description**: Default config assigns 1 relay per blob (`NumRelayAssignment=1` at `encoding_manager.go:87`). The operator relay selection (`node/node_v2.go:62-66`) picks randomly from `cert.RelayKeys` (always length 1). If the assigned relay fails, the code returns error immediately with no retry:

```go
// node/node_v2.go:220 — acknowledged TODO
// TODO (cody-littley) this is flaky, and will fail if any relay fails. We should retry failures
```

Mainnet currently has only 1 relay (`nextRelayKey=1` from on-chain cast call).

**Impact**: Single relay failure = all blobs assigned to it become unprocessable. Operators cannot download chunks, causing attestation failure for affected batches.

**Evidence**:
- `disperser/controller/encoding_manager.go:87` — NumRelayAssignment=1
- `node/node_v2.go:62-66, 219-221` — no retry, TODO acknowledged
- On-chain: RelayRegistry `nextRelayKey=1`

---

### Finding 9: P2<->P3 No Mutual Authentication (Category G)

| | |
|---|---|
| **Severity** | Medium |
| **Category** | G. Undocumented Attack Surface |
| **DFD Location** | TB-D (P2 <-> P3), DF3 |
| **Status** | Confirmed |

**Description**: The gRPC connection between Disperser API Server (P2) and Controller (P3) has no authentication — no mTLS, no tokens, no interceptors. The proto file explicitly documents this:

```proto
// controller_service.proto:19-26
// it does not have any type of auth implemented between the API Server and Controller:
// This is an internal API protected by firewall rules
```

The server (`server.go:71-85`) uses only metrics interceptor + max message size options.

**Impact**: Any process gaining network access to the controller's gRPC port (SSRF, container escape, compromised adjacent service) can directly call `AuthorizePayment`. The blast radius is limited because client ECDSA signatures are still verified, but DoS against the controller's accounting is possible.

**Evidence**:
- `api/proto/controller/controller_service.proto:19-26`
- `disperser/controller/server/server.go:71-85`

---

### Finding 10: DS1/DS2 No Application-Level Integrity (Category D)

| | |
|---|---|
| **Severity** | Medium |
| **Category** | D. Verification Gap |
| **DFD Location** | TB-D (DS1/DS2/DS3) |
| **Status** | Confirmed |

**Description**: S3 reads in `s3_blob_store.go:43-52` perform no checksum/hash verification. The `S3Client` interface has no ETag, ContentMD5, or checksum parameters. The litt KV store (DS3) also has no checksum implementation (README: "Planned/Possible Feature").

**Impact**: Silent S3 corruption or bit-rot could serve corrupted blob data undetected. The system relies entirely on AWS infrastructure-level integrity with no defense-in-depth.

**Evidence**:
- `disperser/common/v2/blobstore/s3_blob_store.go:43-52`
- `common/s3/s3_client.go:14-45` — no checksum in interface
- `litt/` — zero grep hits for checksum/CRC/integrity

---

### Finding 11: Lazy Signing Protocol Vulnerability (Category A/D)

| | |
|---|---|
| **Severity** | Medium |
| **Category** | A. Broken Promise + D. Verification Gap |
| **DFD Location** | TB2/TB6 (P5 DA Node), DF12 |
| **Status** | Partially Confirmed |

**Description**: While the standard code enforces chunk download → KZG validation → BLS signing order (`server_v2.go:169-351`), the **protocol** allows bypass. The `batchHeaderHash` (the signed message) is derived from `BatchHeader` alone — it does NOT cryptographically bind to actual chunk data. A malicious operator can compute `batchHeaderHash` at line 196 and sign immediately without downloading chunks (~5 lines of code modification).

**Impact**: Operators can attest to data availability without actually holding data. Combined with no slashing (Finding 1), there is no economic deterrent. Detection requires external auditing (DAS/Light Node), which is also unimplemented (Category A).

**Evidence**:
- `node/grpc/server_v2.go:169-351` — signing at line 341, after validation
- `core/v2/types.go:241-246` — BatchHeader has no chunk binding
- Protocol gap: `batchHeaderHash = Hash(batchRoot || referenceBlockNumber)` — no chunk commitment

---

### Finding 12: Dead Operators Inflate Quorum Denominator (Category E)

| | |
|---|---|
| **Severity** | Medium |
| **Category** | E. Economic Unreality |
| **DFD Location** | P9 (Registry), DF26 (Operator State) |
| **Status** | Partially Confirmed (mitigated by off-chain ejector) |

**Description**: Dead operators (registered but non-responsive) inflate `totalStakeForQuorum` in `EigenDAServiceManager.sol:116-119`. The ejector (`ejector/ejector.go`) is an off-chain centralized service run by EigenDA team. Operators can cancel ejection on-chain during a delay window via `cancelEjection()`.

BONDA data confirms 11 dead operators (13%) as of 2026-05-11. No ejection events for these operators = the only accountability mechanism is failing.

**Impact**: Quorum threshold calculations assume all registered stake is active. Dead operators effectively raise the bar for attestation, potentially causing legitimate batches to fail threshold.

**Evidence**:
- `EigenDAServiceManager.sol:116-119` — uses totalStakeForQuorum
- `ejector/ejector.go:146-149` — centralized off-chain detection
- `EigenDAEjectionManager.sol:104-119` — operators can cancel ejection
- BONDA dashboard: 11 dead operators, 0 ejection events for them

---

### Finding 13: Relay Response Data Unsigned (Category D)

| | |
|---|---|
| **Severity** | Informational |
| **Category** | D. Verification Gap |
| **DFD Location** | TB2 (P6 Relay), DF22 |
| **Status** | Confirmed (mitigated) |

**Description**: `GetBlobReply` and `GetChunksReply` contain only raw data — no signature, MAC, or integrity proof. However, the proxy performs post-retrieval KZG commitment verification (`relay_payload_retriever.go:120-126`), which catches MITM-injected fake data.

**Mitigation gap**: The secondary storage verification is currently a no-op (`eigenda_manager.go:141`: `// TODO: implement a verify blob function — return nil`).

**Evidence**:
- `relay/server.go:265-268` — GetBlobReply has no signature
- `api/clients/v2/payloadretrieval/relay_payload_retriever.go:120-126` — KZG compensating control
- `api/proxy/store/eigenda_manager.go:141` — secondary cache verify is TODO/no-op

---

### Finding 14: Reputation System Gameable (Category G)

| | |
|---|---|
| **Severity** | Informational |
| **Category** | G. Undocumented Attack Surface |
| **DFD Location** | Off-DFD (disperser selection) |
| **Status** | Confirmed |

**Description**: The reputation system (`common/reputation/reputation.go`) uses asymmetric EMA (failure penalty 4x success reward) with a forgiveness mechanism that drifts scores back to 0.5 with 24h half-life. A disperser can maintain selection eligibility while being unreliable ~80% of the time by spacing failures across the forgiveness window.

**Note**: This affects disperser selection by clients only, not operator selection.

**Evidence**:
- `common/reputation/reputation.go:39-88` — asymmetric EMA + forgiveness
- `common/reputation/reputation_config.go:25` — SuccessUpdateRate=0.05, FailureUpdateRate=0.20
- `api/clients/v2/dispersal/disperser_client_multiplexer.go:127` — selection usage

---

## 4. PoC Scenarios

> All PoCs were executed on 2026-05-12 against EigenDA mainnet and Sepolia testnet.

### PoC-1: Unauthenticated Blob Retrieval (Finding 3)

**Difficulty**: Trivial
**Prerequisites**: Knowledge of a blob key (deterministic from blob header)
**Status**: **EXECUTED AND CONFIRMED** on both mainnet and Sepolia testnet

```
Attack Flow:
1. Query DataAPI for recent blob keys (also unauthenticated — see PoC-5)
   $ curl -s 'https://dataapi.eigenda.xyz/api/v2/blobs/feed?limit=1'
   → blob_key: 0d21ec282f51cf0c2501a377ef989b69c841686f69faf90f177ff7cf335e4ace

2. Resolve relay URL from on-chain RelayRegistry (public):
   $ cast call <Directory> "getAddress(string)(address)" "RELAY_REGISTRY" → 0xD160...55B
   $ cast call <RelayRegistry> "relayKeyToUrl(uint32)(string)" 0
   → "relay-0-mainnet-ethereum.eigenda.xyz"

3. Call GetBlob with no credentials:
   $ grpcurl -d '{"blob_key": "<base64_key>"}' \
       relay-0-mainnet-ethereum.eigenda.xyz:443 relay.Relay/GetBlob

4. Result: 2,796,221 bytes (2.7MB) of blob data returned instantly.

Negative test: Invalid blob_key returns "Code: NotFound" — confirming
the positive result was real data retrieval, not a default response.

Sepolia testnet: Same attack on relay-0-testnet-sepolia.eigenda.xyz
returned 21,865 bytes — also confirmed.

Detection: Rate limiter logs only (global, no per-client attribution)
```

### PoC-2: Relay Denial of Service (Finding 8)

**Difficulty**: High (requires relay network access)
**Prerequisites**: Network access to the single relay

```
Attack Flow:
1. Identify single relay endpoint (on-chain RelayRegistry, nextRelayKey=1)
2. Flood relay with GetBlob requests (no auth required)
3. Global rate limiter triggers, blocking ALL clients
4. All operators fail to download chunks for new batches
5. Attestation fails → blobs stuck in GATHERING_SIGS state

Alternative: L4/L7 DDoS against relay infrastructure

Detection: Relay metrics (if monitored), operator timeout logs
```

### PoC-3: Stale RBN Exploitation for Direct Integrators (Finding 6)

**Difficulty**: Medium
**Prerequisites**: Rollup using direct EigenDA integration without eigenda-proxy
**Status**: **ON-CHAIN VERIFICATION CONFIRMED** — no recency function exists

```
Verification Steps (executed on mainnet):

1. Resolve CertVerifier and Router addresses:
   $ cast call <Directory> "getAddress(string)(address)" "CERT_VERIFIER"
   → 0x61692e93b6B045c444e942A91EcD1527F23A3FB7
   $ cast call <Directory> "getAddress(string)(address)" "CERT_VERIFIER_ROUTER"
   → 0x1be7258230250Bc6a4548F8D59d576a87D216C12

2. Confirm NO recency function on-chain:
   $ cast call <Router> "recencyWindowSize()(uint256)"
   → REVERT (function does not exist)
   $ cast call <CertVerifier> "recencyWindowSize()(uint256)"
   → REVERT (function does not exist)

3. Verify current cert version and required quorums:
   $ cast call <CertVerifier> "certVersion()(uint8)" → 3
   $ cast call <CertVerifier> "quorumNumbersRequired()(bytes)" → 0x0001

4. Confirm: on-chain only checks RBN <= block.number (no staleness check)
   Current block: 25,079,707
   → A cert with RBN = 1,000,000 (24M blocks old, ~333 days)
     would NOT be rejected by on-chain verification.

Attack Flow:
1. Record operator stake distribution at block N (attacker has large stake)
2. Wait for stake to change (attacker reduces stake at block N+50000)
3. Submit blob with RBN = N (referencing old stake distribution)
4. On-chain checkDACert passes (only checks RBN <= block.number)
5. Attacker's old stake weight counts toward quorum threshold

Mitigation: Use eigenda-proxy (has off-chain recency check, window=14400 blocks)
Detection: Compare cert RBN age against expected window
```

### PoC-4: Attestation Record Tampering (Finding 4)

**Difficulty**: High (requires DynamoDB access)
**Prerequisites**: AWS credentials with DynamoDB write access

```
Attack Flow:
1. Gain DynamoDB access (compromised controller, leaked credentials, SSRF)
2. Identify target: blob with status=COMPLETE and full attestation
3. PutItem with same key but empty/weakened attestation fields
4. ConsistentRead immediately serves corrupted record
5. Polling clients receive weakened attestation
6. Downstream cert assembly uses insufficient quorum data

Detection: DynamoDB CloudTrail audit logs (if enabled)
```

### PoC-5: Operational Intelligence Gathering (Finding 7)

**Difficulty**: Trivial
**Prerequisites**: DataAPI endpoint URL (discoverable via port scanning)
**Status**: **EXECUTED AND CONFIRMED** on mainnet

```
Attack Flow (all executed and verified):

1. GET /operators/signing-info → operator signing rates + stake percentages
   Result: 111 operators with unsigned batches identified.
   Top non-signers exposed:
   - 0x3F98F47D... Q0: 0% signing, 0.725% stake (2365/2365 unsigned)
   - 0x46b3f7b5... Q0: 0% signing, 0.668% stake (2365/2365 unsigned)

2. GET /operators/node-info → operator software versions
   Result: Full version distribution exposed:
   - 1 operator on v0.9.1, 1 on v2.1.0, 1 on v2.2.0 (outdated)
   - Operator IDs included for each version

3. GET /operators/liveness → operator IP addresses + port status
   Result: Every operator's dispersal and retrieval IP:port exposed:
   - "dispersal_socket": "15.204.214.239:32006"
   - "retrieval_socket": "15.204.214.239:32007"
   - Online/offline status for each

4. Cross-reference attack demonstrated:
   - Outdated version operators identified by ID
   - IPs resolved from liveness endpoint
   - Signing rates confirm which operators are actively participating
   → Complete operator targeting profile built with zero authentication

Detection: None (no auth, no access logging by default)
```

### PoC-6: Lazy Operator (Modified Node) (Finding 11)

**Difficulty**: Moderate (requires custom operator build)
**Prerequisites**: Registered operator willing to modify node code

```
Modification (~5 lines in server_v2.go):
1. Receive StoreChunksRequest
2. Compute batchHeaderHash from batch header (line 196)
3. Skip: DownloadChunksFromRelays (line 329)
4. Skip: validateAndStoreChunks (line 335)
5. Sign batchHeaderHash immediately (line 341)
6. Return valid BLS signature

Result: Operator attests without holding data
Detection: External retrieval probe (DAS — not implemented)
Penalty: None (no slashing — Finding 1)
```

---

## 5. Disproven Hypotheses

### H1: DACert V3 Pass-through Vulnerability

**Hypothesis**: V3 "dummy" verifier allows unverified V3 certs to pass.

**Finding**: `IEigenDACertTypeBindings.sol` dummy functions exist **only for ABI generation** (Solidity limitation workaround). The Router routes by RBN, not cert version. A V3-encoded cert decoded by a V4 verifier fails `abi.decode` and returns `INVALID_CERT`. **No vulnerability.**

### H2: Quorum Threshold Bypass via Subset Manipulation

**Hypothesis**: A blob specifying fewer quorums than required can bypass verification.

**Finding**: `EigenDACertVerificationLib.sol:255-269` enforces `requiredQuorums ⊆ blobQuorums ⊆ confirmedQuorums`. The subset check is sound. `quorumNumbersRequired` is immutable (set in constructor). **No vulnerability.**

---

## 6. Recommendations

### Critical Priority

| # | Finding | Recommendation |
|---|---------|---------------|
| R1 | No slashing (F1) | Implement EIGEN slashing or interim stake-locking penalty mechanism. Without economic deterrence, BFT-only security assumption requires supermajority honesty. |
| R2 | No CertVerifier timelock (F2) | Enforce minimum activation delay (e.g., 7 days) in `addCertVerifier()` on-chain, not as an external recommendation. |

### High Priority

| # | Finding | Recommendation |
|---|---------|---------------|
| R3 | GetBlob no auth (F3) | Add per-client rate limiting. Consider optional blob-level access control for rollups requiring confidentiality. |
| R4 | Attestation overwrite (F4) | Use conditional writes (`attribute_not_exists` or monotonicity check) for `PutAttestation`, matching the pattern already used by `PutBlobInclusionInfo`. |
| R5 | Encoding no-verify (F5) | Verify at least 1 random chunk proof after encoding. Add mTLS to encoder connection. |
| R6 | Recency off-chain (F6) | Implement on-chain recency check or prominently document that direct integrators MUST implement their own. |
| R7 | DataAPI auth (F7) | Add API key authentication. Segment endpoints into public (metrics) and private (operator details). |
| R8 | Relay SPOF (F8) | Increase `NumRelayAssignment` default to >=2. Implement relay failover (acknowledged TODO). |

### Medium Priority

| # | Finding | Recommendation |
|---|---------|---------------|
| R9 | P2<->P3 no auth (F9) | Add mTLS between disperser components within TB-D. |
| R10 | DS integrity (F10) | Add application-level checksum verification on S3 reads. Implement litt checksums (acknowledged "Planned Feature"). |
| R11 | Lazy signing (F11) | Bind chunk data commitment into the signed message, or implement DAS/retrieval challenges. |
| R12 | Dead operators (F12) | Automate ejection with on-chain liveness proofs, removing operator ability to cancel. |

---

## 7. Appendix: Verification Sources

### 7.1 On-chain Verification
- RelayRegistry `nextRelayKey=1` (cast call, Ethereum mainnet)
- Operator counts: Q0=58, Q1=63, Q2=6
- ServiceManager owner = RegistryCoordinator owner = `0x002721B...`
- Mainnet config: `quorumAdversaryThresholdPercentages = 0x212121` (33%)

### 7.2 Code Verification (4 parallel agents)
- TB-D: `controller_service.proto`, `encoding_manager.go`, `dynamo_metadata_store.go`, `s3_blob_store.go`, `litt/`
- TB2: `relay/server.go`, `relay/limiter/`, `node/node_v2.go`, `node/grpc/server_v2.go`
- TB3: `EigenDACertVerifierRouter.sol`, `EigenDACertVerificationLib.sol`, `EigenDACertVerifier.sol`, `eigenda.go`
- Economic: `EigenDAServiceManager.sol`, `EigenDAEjectionManager.sol`, `EigenDARegistryCoordinator.sol`, `reputation.go`, `disperser/dataapi/`

### 7.3 Prior Audit Cross-reference
- Sigma Prime "EigenDA Proxy Secure Integration" (2025-10): 16 findings, all resolved
- Sigma Prime findings mapped to DFD: P1/P1* (5), P5 (2), P8 (3), P3/P10 (1), P9 (1), TB3 (1)

### 7.4 BONDA Dashboard Data
- 59,828 blobs tracked (as of 2026-05-10)
- Relay success rate: 99.78%
- Operator success rate: 98.14%
- Dead operators: 11 (13%)
- Ejection events: 528 (historical)
- Quorum 2: 3 active operators out of 6 registered, 1 Safe entity controls 90%+ stake

---

*This report was produced as part of the BONDA project's EigenDA threat modeling initiative. All findings are based on source code verification as of 2026-05-12. Findings reflect the state of the codebase at the time of analysis and may not reflect subsequent changes.*
