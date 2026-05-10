# BONDA EigenDA Mainnet Monitor

EigenDA mainnet의 Data Availability를 독립적으로 검증하는 실시간 모니터링 시스템.

두 오픈소스 레포의 방법론을 통합:
- [eigenda-blob-observer](https://github.com/viv4ld-ctrl/eigenda-blob-observer) — passive observation (DataAPI 전수 수집, relay/operator 검증)
- [eigenda-da-monitor](https://github.com/fl0wizy/eigenda-da-monitor) — active probing (HHI, ejection, survival curve)

## 현재 배포 상태

| 서비스 | URL |
|---|---|
| Grafana | http://34.118.200.136:3000 (로그인 불필요) |
| API | http://34.118.200.136:8080/api/status |
| GCP SSH | `gcloud compute ssh test --zone=us-central1-c` |

> GCP `devnet-mvp/test` (e2-medium) VM에서 docker compose 4컨테이너로 가동 중.

---

## Quick Start (로컬)

### 사전 요구
- Docker + Docker Compose
- Alchemy mainnet API key ([무료 발급](https://dashboard.alchemy.com/))

### 실행

```bash
cd eigenda

# 1. 환경변수 설정
cp .env.example .env

# 2. .env 편집 — ETH_RPC_URL만 수정
#    ETH_RPC_URL=https://eth-mainnet.g.alchemy.com/v2/YOUR_API_KEY

# 3. 실행
docker compose up -d

# 4. 확인
curl http://localhost:8080/api/status    # API
open http://localhost:3000               # Grafana
```

### Active Probing (선택)

자체 blob을 mainnet에 제출하려면 추가 설정 필요:

```bash
# .env에 추가
WRITE_PROBER_ENABLED=true
DISPERSER_PRIVATE_KEY=YOUR_HEX_PRIVATE_KEY   # ETH 지갑 (소량 ETH 필요)
WRITE_PROBER_INTERVAL=15m

# PaymentVault에 on-demand deposit (한 번만)
cast send 0xb2e7ef419a2a399472ae22ef5cfccb8be97a4b05 \
  "depositOnDemand(address)" YOUR_ADDRESS \
  --value 0.01ether --private-key YOUR_KEY \
  --rpc-url YOUR_ETH_RPC
```

비용: blob당 ~0.000002 ETH, 15분 간격 6주 ≈ 0.008 ETH

---

## Architecture

Go single binary (7 workers) + eigenda-proxy + TimescaleDB + Grafana

### Workers

| # | Worker | 역할 | 주기 |
|---|---|---|---|
| 1 | BlobCollector | DataAPI `/blobs/feed` mainnet blob 전수 수집 | 3s |
| 2 | RelayVerifier | relay gRPC `GetBlob` + KZG 검증 (10 parallel) | continuous |
| 3 | OperatorVerifier | 86+ operator gRPC `GetChunks` 전수 스캔, Reed-Solomon 복구가능성 판정 | continuous |
| 4 | Reverifier | 10개 log-spaced 나이 버킷 (5m~15d) 재검증 | 5min |
| 5 | WriteProber | eigenda-proxy 경유 blob 제출 (active probing) | 15min |
| 6 | StakeIndexer | 온체인 StakeRegistry → quorum별 HHI 계산 | 1h |
| 7 | EjectionIndexer | `OperatorEjected` eth_getLogs 인덱싱 | 12s |

### 데이터 흐름

```
                     ┌──────────────────────────────────────────┐
                     │             External Sources             │
                     ├──────────────────────────────────────────┤
                     │  DataAPI REST   (blob feed, cert, attest)│
                     │  Relay gRPC     (GetBlob, TLS)           │
                     │  Operator gRPC  (GetChunks, v2 port)     │
                     │  Ethereum L1    (eth_call, eth_getLogs)   │
                     │  Disperser      (via eigenda-proxy)      │
                     └──────────────┬───────────────────────────┘
                                    │
┌───────────────────────────────────▼──────────────────────────┐
│                    Prober (Go, 7 workers)                    │
│                                                              │
│  BlobCollector ──→ observed_blobs                            │
│  RelayVerifier ──→ retrieval_probes (+ KZG)                  │
│  OperatorVerifier ──→ operator_probes (+ recoverability)     │
│  Reverifier ──→ retrieval_probes (age-bucketed re-probe)     │
│  WriteProber ──→ eigenda-proxy ──→ Disperser ──→ observed_blobs │
│  StakeIndexer ──→ stake_snapshots (HHI)                      │
│  EjectionIndexer ──→ ejection_events                         │
│                                                              │
│  JSON API (:8080)                                            │
└───────────────────────────┬──────────────────────────────────┘
                            │
              ┌─────────────▼─────────────┐
              │  TimescaleDB (8 tables)   │
              └─────────────┬─────────────┘
                            │
              ┌─────────────▼─────────────┐
              │  Grafana (3 dashboards)   │
              └───────────────────────────┘
```

### Docker Compose 구성

| 컨테이너 | 이미지 | 역할 |
|---|---|---|
| timescaledb | `timescale/timescaledb:latest-pg16` | 시계열 DB |
| eigenda-proxy | `ghcr.io/layr-labs/eigenda-proxy:latest` | blob 제출 대행 (KZG + 서명 + payment) |
| prober | 자체 빌드 (Go) | 7 workers + API 서버 |
| grafana | `grafana/grafana:latest` | 대시보드 |

---

## API Endpoints

| Endpoint | 설명 |
|---|---|
| `GET /healthz` | 헬스체크 |
| `GET /api/status` | 시스템 상태 (1h relay/operator 성공률, blob 수) |
| `GET /api/survival` | Data Survival Curve (나이별 검색 성공률, Wilson 95% CI) |
| `GET /api/operators` | operator별 성공률, dead operator, 평균 chunks |
| `GET /api/relays` | relay별 성공률, 지연 |
| `GET /api/hhi?hours=168` | HHI 시계열 (quorum별 stake 집중도) |
| `GET /api/ejections?limit=100` | operator ejection 이벤트 |
| `GET /api/probes?limit=50` | 최근 relay probe 로그 |

## Grafana Dashboards

3개 대시보드가 자동 provisioning됨:

1. **EigenDA Overview** — blob 수집 현황, relay 성공률/지연, survival curve, attestation
2. **EigenDA Threat Model** — 위협 카테고리별 메트릭 (promise breach, trust outsourcing, authority concentration, verification gaps, economic assumptions, version gaps)
3. **EigenDA Operators** — operator별 성공률, dead operator, chunks

> datasource type: `grafana-postgresql-datasource` (Grafana 13 필수)

## 디렉토리 구조

```
eigenda/
├── cmd/prober/main.go              # 엔트리포인트
├── internal/
│   ├── config/                     # 환경변수 파싱 (7 worker 토글)
│   ├── db/                         # TimescaleDB 쿼리 + 마이그레이션
│   ├── dataapi/                    # EigenDA DataAPI REST 클라이언트
│   ├── relay/                      # Relay gRPC 클라이언트
│   ├── operator/                   # Operator discovery + GetChunks gRPC
│   ├── registry/                   # RelayRegistry 온체인 해석
│   ├── contracts/                  # StakeRegistry, EjectionManager, Directory ABI
│   ├── kzg/                        # KZG commitment 검증
│   ├── worker/                     # 7개 worker 구현
│   └── api/                        # JSON API 서버
├── grafana/                        # 대시보드 JSON + provisioning
├── docs/                           # 수집 명세, 데이터 수집 현황
├── docker-compose.yml              # TimescaleDB + eigenda-proxy + Prober + Grafana
├── Dockerfile
├── .env.example
└── Makefile
```

## 환경변수

| 변수 | 필수 | 설명 |
|---|---|---|
| `ETH_RPC_URL` | **필수** | Ethereum mainnet RPC (Alchemy/Infura) |
| `TIMESCALEDB_URL` | docker compose 시 자동 | PostgreSQL 접속 문자열 |
| `DATAAPI_BASE_URL` | 기본값 있음 | `https://dataapi.eigenda.xyz/api/v2` |
| `COLLECTOR_ENABLED` | 기본 true | BlobCollector on/off |
| `RELAY_VERIFIER_ENABLED` | 기본 true | RelayVerifier on/off |
| `OPERATOR_VERIFIER_ENABLED` | 기본 true | OperatorVerifier on/off |
| `STAKE_INDEXER_ENABLED` | 기본 true | StakeIndexer on/off |
| `EJECTION_INDEXER_ENABLED` | 기본 true | EjectionIndexer on/off |
| `WRITE_PROBER_ENABLED` | 기본 **false** | WriteProber on/off (ETH 소모) |
| `WRITE_PROBER_INTERVAL` | 기본 5m | WriteProber 제출 간격 |
| `EIGENDA_PROXY_URL` | docker compose 시 자동 | eigenda-proxy 주소 |
| `DISPERSER_PRIVATE_KEY` | WriteProber 사용 시 | blob 제출 서명용 ETH 지갑 private key |

전체 목록: [.env.example](.env.example)

## 관련 문서

- [수집 명세 (eigenda-monitor-spec.md)](docs/eigenda-monitor-spec.md) — API/proto/contract 기반 상세 스펙
- [데이터 수집 현황 (eigenda-data-collection.md)](docs/eigenda-data-collection.md) — 지표 설명 + 활용 계획

## Tech Stack

- **Go 1.26** — single binary, 7 goroutines
- **TimescaleDB** — 시계열 hypertable (8 tables)
- **Grafana 13** — 자동 provisioning 대시보드
- **eigenda-proxy** — blob 제출 대행 (KZG commitment + 서명 + on-demand payment)
- **gRPC** — EigenDA relay + operator 직접 통신
- **EigenDA DataAPI v2** — blob feed, certificate, attestation
