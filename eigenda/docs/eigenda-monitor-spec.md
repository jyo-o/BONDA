# EigenDA Mainnet Monitor — 수집 명세

## 1. 시스템 개요

EigenDA V2 mainnet의 Data Availability 보장을 독립적으로 검증하는 모니터링 시스템.

- **Passive Observation**: 네트워크에 제출되는 모든 blob을 수집하고, relay/operator 경로로 실제 retrieval 가능 여부를 검증
- **Active Probing**: 자체 blob을 Disperser에 제출한 뒤, 시간 경과에 따른 retrievability를 추적

GitHub: https://github.com/jyo-o/BONDA

---

## 2. 데이터 수집 경로

### 2.1 DataAPI (REST)

EigenDA DataAPI V2 (`https://dataapi.eigenda.xyz/api/v2`)에서 blob 메타데이터를 수집한다.

| 엔드포인트 | 수집 데이터 | 용도 |
|---|---|---|
| `GET /blobs/feed` | blob_key, account_id, blob_status, blob_size_bytes, requested_at, expiry_unix_sec, BlobCommitments (G1 X/Y), QuorumNumbers, PaymentMetadata | 전수 수집, 모니터 대상 목록 |
| `GET /blobs/{key}/certificate` | BlobCertificate.RelayKeys[], BlobHeader, Signature | relay 할당 확인, retrieval 대상 결정 |
| `GET /blobs/{key}/attestation-info` | QuorumResults (quorum별 signing_stake_percentage), NonSignerPubKeys[] | operator 서명 참여율 측정 |

### 2.2 Relay (gRPC, TLS)

EigenDA Relay 노드에 직접 blob 데이터를 요청한다. Relay URL은 온체인 `RelayRegistry.relayKeyToUrl(uint32)` 호출로 resolve한다.

| RPC | 요청 | 응답 | 측정 지표 |
|---|---|---|---|
| `relay.Relay/GetBlob` | `GetBlobRequest { blob_key: bytes }` | `GetBlobReply { blob: bytes }` | success/fail, latency_ms, data_size_bytes |

- Relay는 EigenDA의 CDN 역할. 현재 mainnet에 relay_key=0 하나만 운영 중 (SPOF)
- 응답 데이터는 KZG commitment 검증에 사용

### 2.3 Operator (gRPC, insecure)

EigenDA Operator 노드에 직접 erasure-coded chunk를 요청한다. Operator 목록은 온체인 `IndexRegistry` + `SocketRegistry`에서 discovery한다.

| RPC | 요청 | 응답 | 측정 지표 |
|---|---|---|---|
| `validator.Retrieval/GetChunks` | `GetChunksRequest { blob_key: bytes, quorum_id: uint32 }` | `GetChunksReply { chunks: []bytes, chunk_encoding_format }` | success/fail, latency_ms, chunks_returned |

- V2 retrieval port (socket index 3) 사용
- Operator socket 형식: `host:dispersal_port;retrieval_v1_port;dispersal_v2_port;retrieval_v2_port`
- 현재 mainnet 86+ operator 전수 스캔

### 2.4 Disperser (gRPC via eigenda-proxy)

자체 blob을 EigenDA mainnet에 제출한다. eigenda-proxy가 KZG commitment 계산, payment 서명, dispersal polling을 처리한다.

| 동작 | 설명 |
|---|---|
| Payload 생성 | 128 KiB random bytes, 32-byte field element 단위로 high byte zeroed (BN254 modulus 제약) |
| 제출 | `POST /put?commitment_mode=standard` → eigenda-proxy → Disperser gRPC `DisperseBlob` |
| 확인 | proxy가 내부적으로 `GetBlobStatus` polling → COMPLETE 시 DA certificate 반환 |
| 비용 | On-demand payment: `pricePerSymbol × max(numSymbols, minNumSymbols)` ≈ 0.00000183 ETH/blob |

### 2.5 Ethereum L1 (eth_call, eth_getLogs)

온체인 컨트랙트 조회로 operator stake 정보와 ejection 이벤트를 수집한다.

| 컨트랙트 | 주소 (mainnet) | 조회 |
|---|---|---|
| EigenDADirectory | `0x64AB2e9A86FA2E183CB6f01B2D4050c1c2dFAad4` | `getAddress(name)` → 하위 컨트랙트 주소 resolve |
| IndexRegistry | Directory에서 resolve | `totalOperatorsForQuorum(uint8)`, `getLatestOperatorUpdate(uint8, uint32)` |
| SocketRegistry | Directory에서 resolve | `getOperatorSocket(bytes32)` |
| StakeRegistry | Directory에서 resolve | `getCurrentStake(bytes32 operatorId, uint8 quorumNumber) → uint96` |
| EjectionManager | Directory에서 resolve | event `OperatorEjected(bytes32 indexed operatorId, uint8 quorumNumber)` |
| RelayRegistry | Directory에서 resolve | `relayKeyToUrl(uint32) → string` |
| PaymentVault | `0xb2e7ef419a2a399472ae22ef5cfccb8be97a4b05` | `getOnDemandTotalDeposit(address)` |
| CertVerifierRouter | `0x2ea418AE1852bfC79e18B37E55F278F9c598AA08` | cert verification용 |

---

## 3. 측정 지표

### 3.1 Retrieval Availability

| 지표 | 정의 | 계산 |
|---|---|---|
| Relay Success Rate | relay GetBlob 성공 비율 | `COUNT(success) / COUNT(*)` per time window |
| Relay Latency (p50/p95) | relay 응답 시간 분포 | percentile aggregation |
| Per-Relay Breakdown | relay_key별 성공률/지연 | group by relay_key |

### 3.2 Operator Availability & Recoverability

| 지표 | 정의 | 계산 |
|---|---|---|
| Operator Success Rate | operator GetChunks 성공 비율 | `COUNT(success) / COUNT(*)` |
| Blob Recoverability | blob당 복구 가능 여부 | `SUM(chunks_returned) >= 1024` (Reed-Solomon threshold) |
| Dead Operators | 5회 연속 실패 operator | blacklist threshold |
| Per-Operator Breakdown | operator별 성공률/지연/평균 chunks | group by operator_id |

EigenDA는 blob을 8192개 chunk로 Reed-Solomon 인코딩한다. **1024개 이상의 chunk를 수집할 수 있으면 원본 데이터 복구 가능** (1/8 threshold).

### 3.3 Data Survival Curve

| 지표 | 정의 | 계산 |
|---|---|---|
| Age-bucketed Success Rate | 나이 구간별 relay retrieval 성공률 | 12시간 버킷으로 group by |
| Wilson 95% CI | 각 버킷의 신뢰구간 | z=1.96, n≥10일 때만 유효 |

**나이 버킷 (Reverifier 측정 구간)**:

```
5min → 30min → 2h → 8h → 24h → 72h → 7d → 13d → 14d → 15d
```

EigenDA는 ~14일의 DA window를 보장한다고 명시한다. 이 곡선이 14일 이전에 꺾이면 DA 보장 미이행의 증거가 된다.

### 3.4 Dispersal Performance (Active Probing)

| 지표 | 정의 | 계산 |
|---|---|---|
| Dispersal Latency | blob 제출 → DA certificate 수신까지 시간 | `dispersal_latency_ms` |
| Dispersal Success Rate | 제출 시도 대비 성공 비율 | `is_self_dispersed = true` blobs |

자체 제출한 blob은 `is_self_dispersed` 플래그로 표시되며, 이후 동일한 relay/operator 검증 파이프라인을 거친다.

### 3.5 Stake Concentration (HHI)

| 지표 | 정의 | 계산 |
|---|---|---|
| HHI per Quorum | Herfindahl-Hirschman Index | `Σ(stake_pct%)²` per quorum |
| Operator Count | quorum별 활성 operator 수 | count from IndexRegistry |
| Top-N Stake Share | 상위 N개 operator의 누적 stake 비중 | order by stake DESC |

**HHI 기준** (미 법무부 M&A 심사):
- < 1,500: 경쟁적 (분산)
- 1,500 ~ 2,500: 적당히 집중
- \> 2,500: 고도 집중

현재 mainnet: quorum 0/1은 분산 (HHI ~850~900), **quorum 2는 고도 집중 (HHI 4245, operator 5개)**.

### 3.6 Attestation Participation

| 지표 | 정의 | 계산 |
|---|---|---|
| Signing Stake % | quorum별 서명 참여 stake 비율 | DataAPI attestation-info |
| Non-Signer Count | 서명에 불참한 operator 수 | len(NonSignerPubKeys) |

서명 참여율이 높은데 실제 retrieval 성공률이 낮으면, attestation이 실제 가용성을 반영하지 못하고 있다는 의미.

### 3.7 Ejection Events

| 지표 | 정의 | 계산 |
|---|---|---|
| Ejection Count | operator 퇴출 빈도 | eth_getLogs OperatorEjected |
| Ejection vs Dead Operators | 무응답 operator 대비 실제 퇴출 비율 | dead_count vs ejection_count |

---

## 4. 저장 스키마 (TimescaleDB)

| 테이블 | 유형 | 주요 컬럼 |
|---|---|---|
| `eigenda.observed_blobs` | reference | blob_key (PK), account_id, blob_status, blob_size_bytes, requested_at, expiry_unix_sec, commitment_x/y, quorum_numbers, is_self_dispersed, dispersal_latency_ms |
| `eigenda.retrieval_probes` | hypertable | probe_time, blob_key, blob_age_hours, relay_key, success, latency_ms, data_size_bytes, kzg_verified, kzg_error |
| `eigenda.operator_probes` | hypertable | probe_time, blob_key, blob_age_hours, operator_id, operator_socket, quorum_id, success, latency_ms, chunks_returned |
| `eigenda.attestation_snapshots` | hypertable | snapshot_time, blob_key, quorum_number, total_nonsigners, signing_stake_percentage |
| `eigenda.stake_snapshots` | hypertable | snapshot_time, quorum_id, total_stake, operator_count, hhi |
| `eigenda.stake_snapshot_operators` | hypertable | snapshot_time, quorum_id, operator_id, stake, stake_pct |
| `eigenda.ejection_events` | hypertable | event_time, block_number, tx_hash, log_index, operator_id, quorum_number |
| `eigenda.indexer_cursors` | reference | indexer_name (PK), last_block |

---

## 5. API

JSON API (`http://<host>:8080`)

| 엔드포인트 | 응답 |
|---|---|
| `GET /api/status` | relay/operator 성공률(1h), blob 수, system status |
| `GET /api/survival` | 나이별(12h 버킷) 성공률 + Wilson 95% CI |
| `GET /api/operators` | operator별 성공률, 지연, chunks, dead 목록 |
| `GET /api/relays` | relay별 성공률, 지연 |
| `GET /api/hhi?hours=168` | HHI 시계열 per quorum |
| `GET /api/ejections?limit=100` | ejection 이벤트 목록 |
| `GET /api/probes?limit=50` | 최근 relay probe 로그 |

---

## 6. Grafana 대시보드

3개 대시보드가 자동 provisioning된다.

### EigenDA Overview
전체 현황. Relay/Operator 성공률, Latency, Survival Curve, Attestation, Per-Relay 통계.

### EigenDA Threat Model
위협 카테고리별 패널:
- **Row A**: Promise Breach — survival curve, blob recoverability
- **Row B**: Trust Outsourcing — relay vs operator success divergence
- **Row C**: Authority Concentration — HHI time series, top-5 stake
- **Row D**: Verification Gaps — KZG results, attestation vs retrieval
- **Row E**: Economic Assumptions — ejection events, dead operators
- **Row F**: Version Gaps — operator connection error patterns

### EigenDA Operators
Operator 상세. 전체 operator 테이블, dead operator, per-operator 성공률 추이.

---

## 7. 아키텍처

```
┌─ External (read-only) ──────────────────────────────────┐
│  DataAPI REST        (blob feed, certificates, attest)  │
│  Relay gRPC          (GetBlob, TLS)                     │
│  Operator gRPC       (GetChunks, insecure, v2 port)     │
│  Ethereum L1 RPC     (eth_call, eth_getLogs)            │
└─────────────────────────────────────────────────────────┘
                         ↕
┌─ Prober (Go, single binary) ───────────────────────────┐
│  W1 BlobCollector        DataAPI polling (3s)           │
│  W2 RelayVerifier        10-parallel GetBlob + KZG      │
│  W3 OperatorVerifier     86+ sequential GetChunks       │
│  W4 Reverifier           10 age-bucket re-probe (5min)  │
│  W5 WriteProber          eigenda-proxy PUT (15min)      │
│  W6 StakeIndexer         StakeRegistry → HHI (1h)       │
│  W7 EjectionIndexer      eth_getLogs (12s)              │
│  API Server              JSON :8080                     │
└─────────────────────────────────────────────────────────┘
                         ↕
┌─ Storage ───────────────────────────────────────────────┐
│  TimescaleDB  (8 tables, hypertables for time-series)   │
└─────────────────────────────────────────────────────────┘
                         ↕
┌─ Visualization ─────────────────────────────────────────┐
│  Grafana (3 dashboards, auto-provisioned)               │
└─────────────────────────────────────────────────────────┘
```

---

## 8. 로컬 실행

```bash
git clone https://github.com/jyo-o/BONDA.git
cd BONDA/eigenda
cp .env.example .env
# .env에서 ETH_RPC_URL 설정 (Alchemy/Infura mainnet key)
docker compose up -d
# Grafana: http://localhost:3000
# API:     http://localhost:8080/api/status
```
