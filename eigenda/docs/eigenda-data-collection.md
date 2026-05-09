# EigenDA Mainnet 데이터 수집 현황

## 개요

EigenDA mainnet에서 발생하는 모든 blob의 가용성을 실시간으로 측정하고 있다. 측정 결과는 TimescaleDB에 시계열로 누적되며, Grafana 대시보드와 JSON API로 조회할 수 있다.

측정의 목적은 EigenDA가 실제로 약속한 가용성(~14일 DA 윈도우)을 지키고 있는지, operator/relay 인프라가 건전한지를 **동적으로 검증**하는 것이다. L2BEAT 등 기존 도구는 정적 분석만 제공하며, 실시간 retrieval 검증은 수행하지 않는다.

---

## 수집 대상과 측정 방법

### 1. Blob 전수 수집

EigenDA DataAPI `/blobs/feed` 엔드포인트를 3초마다 폴링하여 mainnet에 제출된 모든 blob을 수집한다. blob당 다음 정보를 기록한다:

- blob_key (고유 식별자)
- account_id (제출자)
- blob_size, commitment (X/Y 좌표)
- quorum 할당 정보
- 만료 시각 (expiry_unix_sec)

### 2. Relay Retrieval 검증

수집된 blob마다 DataAPI에서 certificate를 조회하고, 할당된 relay에 gRPC `GetBlob`으로 실제 데이터를 요청한다. 10개 요청을 병렬로 처리한다.

**기록 항목**: 성공/실패, 응답 지연(ms), 데이터 크기, relay_key

relay는 EigenDA에서 blob 데이터를 빠르게 꺼내는 CDN 역할을 한다. 현재 mainnet에는 relay가 1개(relay_key=0)만 운영되고 있어서, 이 relay가 단일 장애점(SPOF)이 된다.

### 3. Operator Chunk 검증

blob마다 mainnet에 등록된 86+개 operator 전체를 순회하며 gRPC `GetChunks`로 chunk를 요청한다.

**기록 항목**: operator별 성공/실패, 응답 지연, 반환된 chunk 수

EigenDA는 Reed-Solomon 인코딩을 사용하여 blob을 8192개 chunk로 분할한다. 이 중 **1024개 이상**의 chunk를 수집할 수 있으면 원본 데이터를 복구할 수 있다. 이 임계값을 기준으로 blob의 복구 가능성(RECOVERABLE / AT_RISK)을 판정한다.

### 4. 시간 경과별 재검증 (Reverifier)

blob이 시간이 지나도 여전히 검색 가능한지를 확인한다. 5분마다 다음 10개 나이 구간에서 랜덤으로 blob을 선택하여 relay에 재검색한다:

| 나이 | 검증 목적 |
|---|---|
| 5분, 30분 | 제출 직후 가용성 |
| 2시간, 8시간 | 단기 retention |
| 1일, 3일 | 중기 retention |
| 7일 | 장기 retention |
| 13일, 14일, 15일 | DA 윈도우 경계 (만료 직전/직후) |

이 데이터가 축적되면 **Data Survival Curve** — 나이별 검색 성공률 곡선 — 을 그릴 수 있다. EigenDA는 ~14일의 DA 보장 기간을 주장하는데, 이 곡선이 14일 이전에 꺾이면 약속 불이행의 증거가 된다.

현재는 수집 시작 직후라 초기 나이 구간의 데이터만 존재한다. **7~12일 후부터 장기 구간 데이터가 의미있어진다.**

### 5. Stake 집중도 (HHI)

1시간마다 온체인 StakeRegistry 컨트랙트에서 모든 operator의 stake를 조회하고, quorum별 HHI(Herfindahl-Hirschman Index)를 계산한다.

HHI = Σ(각 operator의 stake 점유율%)²

| HHI 범위 | 의미 |
|---|---|
| < 1,500 | 분산 (경쟁적) |
| 1,500 ~ 2,500 | 적당히 집중 |
| > 2,500 | 고도 집중 |

EigenDA가 "operator 86개"라고 해도 stake가 소수에 집중되면 실질적 탈중앙화 수준이 낮다. HHI로 이를 수치화한다.

### 6. Operator Ejection 이벤트

EjectionManager 컨트랙트의 `OperatorEjected` 이벤트를 `eth_getLogs`로 인덱싱한다. operator가 네트워크에서 강제 퇴출되는 빈도와 대상을 추적한다.

*현재 Alchemy free tier의 블록 범위 제한으로 수집이 제한되고 있다.*

### 7. Attestation 서명률

blob마다 DataAPI attestation-info에서 quorum별 서명 참여 stake 비율을 조회한다. 서명률이 높은데 실제 retrieval 성공률이 낮으면, attestation이 실제 가용성을 반영하지 못하고 있다는 의미이다.

---

## 데이터 저장 구조

TimescaleDB에 다음 테이블로 저장된다:

| 테이블 | 내용 | 유형 |
|---|---|---|
| `eigenda.observed_blobs` | 수집된 모든 blob 메타데이터 | reference |
| `eigenda.retrieval_probes` | relay 검증 결과 (시간, 성공여부, 지연, 크기) | 시계열 |
| `eigenda.operator_probes` | operator chunk 검증 결과 | 시계열 |
| `eigenda.attestation_snapshots` | quorum별 서명 참여율 | 시계열 |
| `eigenda.stake_snapshots` | quorum별 HHI, 총 stake, operator 수 | 시계열 |
| `eigenda.stake_snapshot_operators` | operator별 stake 비중 | 시계열 |
| `eigenda.ejection_events` | operator 퇴출 이벤트 | 시계열 |
| `eigenda.indexer_cursors` | 체인 인덱서 진행 상태 | reference |

시계열 테이블은 TimescaleDB hypertable로 관리되어 시간 범위 쿼리가 효율적이다.

---

## 대시보드

Grafana에 3개 대시보드가 구성되어 있다. 로그인 없이 접근 가능하다.

### EigenDA Overview

전체 현황 요약.

- **Status**: 총 blob 수, 최근 1시간 수집량, relay/operator 성공률
- **Relay Probes Over Time**: 시간대별 성공/실패 추이
- **Relay Latency**: p50/p95 응답 지연
- **Data Survival Curve**: 나이별 검색 성공률
- **Attestation Signing %**: quorum별 서명 참여율 추이
- **Recent Probes**: 최근 relay 검증 로그
- **Per-Relay Stats**: relay별 누적 성공률

### EigenDA Threat Model

위협 모델링 카테고리별 메트릭.

- **약속 미이행**: Survival Curve, blob 복구가능성 비율
- **신뢰 외주화**: relay 성공률 vs operator 직접 성공률 괴리
- **권한 집중**: HHI 시계열, 상위 5 operator stake 비중
- **검증 공백**: KZG 검증 결과, attestation vs 실제 retrievability
- **경제적 가정**: ejection 빈도, dead operator 수
- **버전 격차**: operator 연결 에러 패턴

### EigenDA Operators

operator별 상세.

- 총 operator 수, dead operator 수, 평균 chunk 수
- operator별 성공률/지연/chunk 테이블
- 성공률 하위 operator 추이

---

## API

JSON API로도 동일한 데이터를 조회할 수 있다.

| 엔드포인트 | 설명 |
|---|---|
| `GET /api/status` | 시스템 상태 (relay/operator 성공률, blob 수) |
| `GET /api/survival` | 나이별 검색 성공률 + Wilson 95% 신뢰구간 |
| `GET /api/operators` | operator별 통계 |
| `GET /api/relays` | relay별 통계 |
| `GET /api/hhi?hours=168` | HHI 시계열 |
| `GET /api/ejections?limit=100` | ejection 이벤트 |
| `GET /api/probes?limit=50` | 최근 probe 로그 |

---

## 향후 활용

- **중간 발표 (05/22)**: 12일치 데이터로 survival curve 시연, HHI 집중도 결과 발표
- **위협 모델링 근거**: 각 위협 카테고리에 대해 정량적 데이터 제시
- **다른 DA와 비교**: 동일한 측정 방법론을 Celestia/Avail/Ethereum DA에 적용하여 cross-DA 비교 대시보드 구축
