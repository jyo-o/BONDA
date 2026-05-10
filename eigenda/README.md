# BONDA EigenDA Mainnet Monitor

EigenDA mainnet의 실시간 가용성을 측정하는 모니터. 두 오픈소스 레포를 통합:
- [eigenda-blob-observer](https://github.com/viv4ld-ctrl/eigenda-blob-observer) — mainnet passive (DataAPI 전수 수집, relay/operator 검증)
- [eigenda-da-monitor](https://github.com/fl0wizy/eigenda-da-monitor) — active probing (HHI, ejection, survival curve, KZG)

## 현재 배포 상태

| 서비스 | URL |
|---|---|
| Grafana | http://34.118.200.136:3000 |
| API | http://34.118.200.136:8080/api/status |
| GCP SSH | `gcloud compute ssh test --zone=us-central1-c` |

> GCP `devnet-mvp/test` VM에서 docker compose로 가동 중. 데이터 수집 진행 중.

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

# 2. .env 편집 — 아래 한 줄만 수정
#    ETH_RPC_URL=https://eth-mainnet.g.alchemy.com/v2/YOUR_API_KEY

# 3. 실행
docker compose up -d

# 4. 확인
curl http://localhost:8080/api/status    # API
open http://localhost:3000               # Grafana (admin/admin)
```

### 동작 확인

```bash
# 수집된 blob 수 (수 분 후 증가하면 정상)
curl -s http://localhost:8080/api/status | python3 -m json.tool

# 기대 출력 예시:
# {
#     "relay_success_rate": 100,
#     "operator_success_rate": 89.5,
#     "total_blobs": 4909,
#     "status": "healthy"
# }
```

---

## Architecture

Single Go binary, 7 concurrent workers:

| # | Worker | 역할 | 주기 |
|---|---|---|---|
| 1 | BlobCollector | DataAPI `/blobs/feed` mainnet blob 전수 수집 | 3s |
| 2 | RelayVerifier | relay gRPC GetBlob + KZG 검증 (10 parallel) | continuous |
| 3 | OperatorVerifier | 86+ operator GetChunks 전수 스캔, Reed-Solomon 복구가능성 판정 | continuous |
| 4 | Reverifier | 10개 log-spaced 나이 버킷으로 시간 경과별 재검증 (5m~15d) | 5min |
| 5 | StakeIndexer | quorum별 operator stake 스냅샷 + HHI(집중도) 계산 | 1h |
| 6 | EjectionIndexer | OperatorEjected 이벤트 eth_getLogs 인덱싱 | 12s |
| 7 | WriteProber | 자체 blob 제출로 write-path 검증 (optional, mainnet ETH 소모) | 5min |

```
DataAPI ──→ [BlobCollector] ──→ TimescaleDB ←── [StakeIndexer]
                                    ↑               ↑
Relay gRPC ←── [RelayVerifier] ─────┘               │
                                                     │
Operator gRPC ←── [OperatorVerifier] ────────────────┘
                                                     │
EjectionManager ←── [EjectionIndexer] ───────────────┘
                                                     │
                               [Reverifier] ─────────┘
                                    │
                              JSON API (:8080) ──→ Grafana (:3000)
```

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
2. **EigenDA Threat Model** — 6개 위협 카테고리별 메트릭 패널
3. **EigenDA Operators** — operator별 성공률, dead operator, chunks

> Grafana에 처음 접속 시 BONDA 폴더에 3개 대시보드가 보여야 합니다.
> 안 보이면 datasource 설정을 확인하세요 (type: `grafana-postgresql-datasource`).

## 디렉토리 구조

```
eigenda/
├── cmd/prober/main.go              # 엔트리포인트
├── internal/
│   ├── config/                     # 환경변수 파싱
│   ├── db/                         # TimescaleDB 쿼리 + 마이그레이션
│   ├── dataapi/                    # EigenDA DataAPI REST 클라이언트
│   ├── relay/                      # Relay gRPC 클라이언트
│   ├── operator/                   # Operator discovery + GetChunks gRPC
│   ├── registry/                   # RelayRegistry 온체인 해석
│   ├── contracts/                  # StakeRegistry, EjectionManager ABI
│   ├── kzg/                        # KZG commitment 검증
│   ├── worker/                     # 7개 worker 구현
│   └── api/                        # JSON API 서버
├── grafana/                        # 대시보드 JSON + provisioning
├── docker-compose.yml              # TimescaleDB + Prober + Grafana
├── Dockerfile
├── .env.example                    # 환경변수 템플릿
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

전체 목록: [.env.example](.env.example)

## Tech Stack

- **Go 1.26** — single binary, 7 goroutines
- **TimescaleDB** — 시계열 hypertable (8 tables)
- **Grafana 13** — 자동 provisioning 대시보드
- **gRPC** — EigenDA relay + operator 직접 통신
- **EigenDA DataAPI v2** — blob feed, certificate, attestation
