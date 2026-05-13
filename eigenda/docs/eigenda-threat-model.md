# EigenDA Threat Model

**Project**: BONDA
**Date**: 2026-05-12
**Scope**: EigenDA v0.9.2+ mainnet
**Method**: DFD 기반 위협 식별 → 자산 매핑 → 공격자 모델 → 리스크 평가

---

## 1. 보호 대상 자산

### 1.1 금융 노출

| 자산 | 규모 | 출처 |
|------|------|------|
| EigenDA TVS (Total Value Secured) | **$670M** | L2BEAT |
| MegaETH (최대 단일 롤업) | ~$600M | CryptoBriefing |
| Celo (bridge-verified) | ~$270M | L2BEAT |
| Fuel Ignition | ~$5M | L2BEAT |
| EigenDA restaked ETH | 3.44M ETH (~$8.6B @$2500) | EigenLayer |
| EigenDA restaked EIGEN | 59M EIGEN | EigenLayer |

**핵심**: EigenDA가 14일간 데이터를 보관하므로, 공격 시점에 14일 내 제출된 모든 blob이 위험. 해당 기간의 롤업 트랜잭션 총액이 실제 노출 규모.

### 1.2 보안 속성별 자산

| 보안 속성 | 파괴 시 결과 | 영향받는 자산 |
|-----------|-------------|-------------|
| **Data Availability** | 롤업이 상태 증명 불가 → bridge 출금 지연/불가 | $670M TVS |
| **Data Integrity** | 잘못된 상태 전이 인정 → bridge에서 부정 출금 | $670M TVS |
| **Liveness** | 새 blob 제출 불가 → 롤업 시퀀싱 중단 또는 L1 fallback | 비용 증가 (calldata 10x) |
| **Censorship Resistance** | 특정 롤업/계정의 blob 거부 | 해당 롤업 사용자 |
| **Accountability** | 악행 operator 식별/처벌 불가 | 장기 시스템 신뢰 |

---

## 2. 공격자 모델

### A1. 악의적 Operator (내부자)

| 항목 | 내용 |
|------|------|
| **동기** | 경제적 이익 (chunk 저장 비용 절감), 경쟁 방해 |
| **능력** | 자기 노드 코드 수정 가능, BLS 키 보유, 등록된 stake |
| **비용** | 낮음 (이미 operator로 등록, ~5줄 코드 수정) |
| **제약** | Slashing 없음 → 경제적 처벌 0, ejection만 가능하나 취소 가능 |

### A2. Disperser 인프라 타협자

| 항목 | 내용 |
|------|------|
| **동기** | 검열, 데이터 조작, 서비스 거부 |
| **능력** | Disperser/Controller/Encoder/Relay/DS1/DS2 접근 (TB-D 내부) |
| **비용** | 높음 (EigenLabs 인프라 침투 필요) |
| **제약** | KZG/BLS 암호학은 우회 불가, 하지만 omission(거부/지연/누락)은 가능 |

### A3. 거버넌스 공격자

| 항목 | 내용 |
|------|------|
| **동기** | 프로토콜 장악, 자금 인출 |
| **능력** | Owner multisig 키 탈취 또는 사회공학 |
| **비용** | 매우 높음 (multisig 과반 타협) |
| **제약** | 온체인 tx 가시성, 하지만 CertVerifier 교체는 1블록 후 적용 가능 |

### A4. 외부 공격자 (네트워크)

| 항목 | 내용 |
|------|------|
| **동기** | 경쟁 DA 프로토콜, 숏 포지션, 랜섬 |
| **능력** | DDoS, 공개 API 정찰, operator 타겟팅 |
| **비용** | 중간 (DDoS 인프라, DataAPI 정찰은 무료) |
| **제약** | 암호학 우회 불가, 내부 접근 없음 |

---

## 3. 위협 시나리오 — 자산 매핑 — 리스크 평가

> **Likelihood**: 공격 비용, 필요 접근 수준, 선행 조건
> **Impact**: 영향받는 자산 규모, 복구 난이도, 연쇄 효과

### T1. Lazy Signing — 서명만 하고 데이터 미보관

| 항목 | 내용 |
|------|------|
| **공격자** | A1 (악의적 Operator) |
| **경로** | P5(DA Node): batchHeaderHash 서명 → chunk 다운로드/검증 스킵 |
| **근거** | batchHeaderHash는 BatchHeader에서만 파생, chunk 데이터 미바인딩 (코드 검증 완료) |
| **선행 조건** | operator 등록 + 노드 코드 수정 (~5줄) |
| **타겟 자산** | Data Availability ($670M TVS) |
| **Impact** | **High** — 14일 내 해당 operator에 할당된 chunk 전부 조회 불가. 충분한 operator가 lazy하면 RS 복구 실패 → 데이터 영구 유실 |
| **Likelihood** | **Medium** — 코드 수정은 쉽지만, slashing 없어도 ejection 리스크 + 평판 리스크. 하지만 DAS가 없어서 **탐지 수단이 없음** |
| **Detection** | **없음** — DAS/Light Node 미구현. 외부 retrieval probe(BONDA)만 유일한 탐지 수단 |
| **Risk** | **Medium** (RS coding rate 1/8 → 서명자의 87.5%가 동시 lazy해야 데이터 유실. 현재 lazy 3.4%, gap 84%p. 장기 누적 리스크는 존재하나 즉시 위협은 낮음) |

### T2. Disperser 검열 — 특정 롤업 blob 거부

| 항목 | 내용 |
|------|------|
| **공격자** | A2 (인프라 타협) 또는 EigenLabs 자체 (법적 압력) |
| **경로** | P2(Disperser API): `validateDispersalRequest`에서 특정 account_id 거부 |
| **근거** | Disperser 1개, EigenLabs 운영, permissioned (`onlyOwner`). 검열 저항 미구현 |
| **선행 조건** | Disperser 접근 또는 EigenLabs 협조 |
| **타겟 자산** | Censorship Resistance, Liveness |
| **Impact** | **High** — 타겟 롤업은 L1 calldata fallback으로 전환해야 하나, OP Stack은 자동 fallback, Orbit은 설정 필요. 비용 10x 증가 |
| **Likelihood** | **Low** — EigenLabs가 의도적으로 검열할 동기 낮음. 법적 강제는 가능하나 선례 없음 |
| **Detection** | blob submission 실패율 모니터링 (BONDA write prober) |
| **Risk** | **Medium** |

### T3. CertVerifier 교체 — 검증 로직 무력화

| 항목 | 내용 |
|------|------|
| **공격자** | A3 (거버넌스 공격자) |
| **경로** | EE3 → P9(Router): `addCertVerifier(malicious, block.number+1)` |
| **근거** | 최소 activation delay 미강제 (코드 검증 완료). 컨트랙트 주석에 "recommend timelock" |
| **선행 조건** | Owner multisig 과반 키 탈취 |
| **타겟 자산** | Data Integrity ($670M TVS) |
| **Impact** | **Critical** — `checkDACert`가 항상 SUCCESS 반환 → 잘못된 cert 통과 → 롤업 bridge에서 부정 인출 가능. **$670M 전액 위험** |
| **Likelihood** | **Very Low** — multisig 타협은 극히 어려움 |
| **Detection** | CertVerifier 컨트랙트 변경 이벤트 온체인 모니터링 |
| **Risk** | **Medium** (Impact Critical × Likelihood Very Low) |

### T4. Relay DDoS — 데이터 가용성 중단

| 항목 | 내용 |
|------|------|
| **공격자** | A4 (외부 공격자) |
| **경로** | P6(Relay): GetBlob 무인증 + global rate limit만 → 대량 요청으로 relay 마비 |
| **근거** | 메인넷 relay 1개 (SPOF), NumRelayAssignment=1, 실패 시 재시도 없음 (TODO 인정) |
| **선행 조건** | relay 엔드포인트 (on-chain 공개) + DDoS 인프라 |
| **타겟 자산** | Data Availability, Liveness |
| **Impact** | **High** — relay 마비 시 operator chunk pull 불가 → 새 배치 attestation 실패 → blob GATHERING_SIGS에서 정체. 기존 blob은 14일 내 조회 불가 |
| **Likelihood** | **Very Low** — relay는 Cloudflare 뒤에 배치 (`104.18.x.x`, `server: cloudflare`, `cf-ray` 헤더 확인). Sepolia 실측에서 단일 머신 1940 RPS (30초, 58,698 요청)에 실패 0건 — Cloudflare가 흡수. 코드 레벨 rate limit(1024/s) 이전에 Cloudflare L3/L4/L7 DDoS mitigation이 적용되므로 원본 서버 직접 공격 불가. 사실상 "Cloudflare를 뚫어야 relay에 도달" |
| **Detection** | relay 응답률/지연 모니터링 (BONDA relay verifier) |
| **Risk** | **Low** (Cloudflare 보호 확인하여 Medium → Low로 하향) |

### T5. Stale RBN 공격 — 과거 stake 분포 악용

| 항목 | 내용 |
|------|------|
| **공격자** | A1 (악의적 Operator 연합) |
| **경로** | TB3: 온체인 recency check 없음 → 과거 RBN으로 cert 제출 → 과거 stake 비율로 threshold 충족 |
| **근거** | 온체인에 `recencyWindowSize` 함수 없음 (cast call revert 확인). 오프체인 proxy에만 존재 |
| **선행 조건** | 직접 통합 롤업 (eigenda-proxy 미사용) + 과거에 높은 stake 보유 |
| **타겟 자산** | Data Integrity |
| **Impact** | **High** — 잘못된 cert가 bridge에 수용되면 부정 출금 가능 |
| **Likelihood** | **Low** — 대부분 rollup이 eigenda-proxy 사용. 직접 통합은 현재 극소수 |
| **Detection** | cert RBN age 모니터링 |
| **Risk** | **Medium** |

### T6. Dead Operator Quorum 희석

| 항목 | 내용 |
|------|------|
| **공격자** | 없음 (구조적 문제) |
| **경로** | 등록됐지만 비활성 operator가 `totalStakeForQuorum` 분모를 늘림 |
| **근거** | BONDA 실측: 11 dead operators (13%), ejection 0건. Signing 0% operator 2개 (Q0 stake 합계 1.39%) |
| **선행 조건** | 없음 (현재 진행 중) |
| **타겟 자산** | Liveness (threshold 미달로 attestation 실패) |
| **Impact** | **Medium** — 현재 dead operator stake 비율이 낮아 threshold 미달 유발 가능성 낮음. 하지만 축적되면 문제 |
| **Likelihood** | **Medium** — 이미 발생 중 |
| **Detection** | operator signing rate 추적 (BONDA operator verifier + DataAPI) |
| **Risk** | **Medium** |

### T7. Attestation 덮어쓰기

| 항목 | 내용 |
|------|------|
| **공격자** | A2 (인프라 타협자) |
| **경로** | DS1(DynamoDB): `PutAttestation` 무조건 덮어쓰기 → 완료된 attestation을 빈 값으로 교체 |
| **근거** | `// Allow overwrite of existing attestation` 주석, 조건부 쓰기 미사용 (코드 검증 완료) |
| **선행 조건** | AWS DynamoDB 접근 (controller 타협 또는 AWS 자격증명 유출) |
| **타겟 자산** | Data Availability |
| **Impact** | **High** — 완료된 blob의 attestation 무효화 → 해당 blob 조회 시 cert 검증 실패 |
| **Likelihood** | **Low** — AWS 인프라 접근 필요 |
| **Detection** | blob status 역행 모니터링 (COMPLETE → 비정상 상태) |
| **Risk** | **Medium** |

### T8. 인코딩 조작

| 항목 | 내용 |
|------|------|
| **공격자** | A2 (인프라 타협자) |
| **경로** | P4(Encoder): 잘못된 chunk/proof 생성 → P3이 무검증 수용 → operator에 배포 |
| **근거** | encoding_manager.go가 encoder 결과 무검증, insecure gRPC (코드 검증 완료) |
| **선행 조건** | encoder 인프라 접근 |
| **타겟 자산** | Data Integrity, Data Availability |
| **Impact** | **Medium** — operator가 KZG 검증에서 reject → 대량 attestation 실패 (liveness 영향). KZG가 건전하면 integrity는 보호됨 |
| **Likelihood** | **Low** — TB-D 내부 접근 필요 |
| **Detection** | operator 측 StoreChunks 거부율 모니터링 |
| **Risk** | **Low** |

---

## 4. 리스크 매트릭스 요약

```
Impact →        Low          Medium         High          Critical
Likelihood ↓
─────────────────────────────────────────────────────────────────
Very Low                                                  T3(거버넌스)
Low                          T5(RBN)        T7(attest)
                             T2(검열)
Low~Medium                   T4(relay DDoS)
Medium           T8(인코딩)   T6(dead op)    T1(lazy sign)
High
─────────────────────────────────────────────────────────────────
```

### 최종 리스크 등급

| Risk Level | 위협 | 핵심 이유 |
|-----------|------|----------|
| **Medium** | T1. Lazy Signing | 탐지 0 + 처벌 0이지만, RS 1/8 coding → 87.5% 동시 담합 필요. 현재 lazy 3.4%. 장기 누적 리스크 |
| **Low** | T4. Relay DDoS | SPOF + 무인증이지만 Cloudflare 뒤에 배치. 1940 RPS 실측에도 실패 0. 원본 서버 직접 도달 불가 |
| **Medium** | T2. Disperser 검열 | 중앙화 SPOF이지만 fallback 존재 |
| **Medium** | T3. CertVerifier 교체 | $670M 전액 위험이지만 multisig 타협 극히 어려움 |
| **Medium** | T5. Stale RBN | 직접 통합 롤업만 해당 (소수) |
| **Medium** | T6. Dead Operator | 현재 진행 중이지만 stake 비율 낮음 |
| **Medium** | T7. Attestation 덮어쓰기 | AWS 접근 필요 |
| **Low** | T8. 인코딩 조작 | TB-D 접근 필요 + KZG가 integrity 보호 |

---

## 5. EigenDA가 알고 있는 것 vs BONDA가 측정해야 하는 것

위 위협들은 대부분 EigenDA 팀이 **인지하고 있는 설계 결정**이다. 코드 주석과 TODO가 이를 증명한다.

BONDA의 가치는 "취약점 발견"이 아니라, **이 설계 결정들이 실제로 얼마나 잘 버티고 있는지를 외부에서 실시간 측정하는 것**이다.

| 위협 | EigenDA의 가정 | BONDA가 측정하는 것 |
|------|---------------|-------------------|
| T1. Lazy Signing | "operator 대다수가 정직" (BFT 가정) | **retrieval probe**: 실제 chunk가 조회 가능한지 operator별 실측 |
| T4. Relay DDoS | "relay가 가용" | **relay verifier**: relay 응답률, 지연, 가용성 실시간 추적 |
| T6. Dead Operator | "ejector가 처리" | **operator status**: signing rate 0% operator 추적, quorum 분모 영향 계산 |
| T2. Disperser 검열 | "EigenLabs가 정직" | **write prober**: 자체 blob 제출 성공률 측정 |
| T5. Stale RBN | "rollup이 proxy 사용" | **cert age**: RBN과 현재 블록 차이 분포 추적 |
| T3. CertVerifier 교체 | "multisig가 안전" | **governance monitor**: CertVerifier/Router 컨트랙트 변경 이벤트 알림 |

---

## 6. 검증 출처

### 코드 검증 (4 병렬 에이전트, 2026-05-12)
- TB-D: `controller_service.proto`, `encoding_manager.go`, `dynamo_metadata_store.go`
- TB2: `relay/server.go`, `node/grpc/server_v2.go`, `node/node_v2.go`
- TB3: `EigenDACertVerifierRouter.sol`, `EigenDACertVerificationLib.sol`
- 경제: `EigenDAServiceManager.sol`, `EigenDAEjectionManager.sol`, `reputation.go`

### 온체인 검증 (cast call, 2026-05-12)
- RelayRegistry: relay 1개 (`relay-0-mainnet-ethereum.eigenda.xyz`)
- CertVerifier: `recencyWindowSize` 함수 없음 (revert)
- Operator: Q0=58, Q1=63, Q2=6

### 실측 PoC (2026-05-12)
- GetBlob 무인증: 메인넷 2.7MB + Sepolia 128B (본인 지갑 `0x6C818...` 제출 blob)
- DataAPI 무인증: 111개 non-signing operator, IP/포트/버전 전면 노출
- Stale RBN: 온체인 recency 함수 부재 확인

### 금융 노출 (웹 조사, 2026-05-12)
- L2BEAT EigenDA: $670M TVS
- EigenLayer: $18B TVL, 1,900 operators
- MegaETH: ~$600M TVL
- Celo: ~$270M TVL (bridge-verified)
