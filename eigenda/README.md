# BONDA EigenDA Mainnet Monitor

EigenDA mainnet의 실시간 가용성 측정 + 위협 모델링 대시보드.

두 개 오픈소스 레포의 장점을 통합:
- **eigenda-blob-observer** (mainnet passive observation) — DataAPI 전수 수집, relay/operator 검증, 복구 가능성 판정
- **eigenda-da-monitor** (active probing) — HHI, ejection 인덱싱, survival curve, KZG 검증

## Architecture

Single Go binary, 7 concurrent workers:

| Worker | Role | Cadence |
|---|---|---|
| BlobCollector | DataAPI `/blobs/feed` 전수 수집 | 3s |
| RelayVerifier | gRPC GetBlob + KZG 검증 | continuous |
| OperatorVerifier | 86+ operator GetChunks 전수 스캔 | continuous |
| Reverifier | 10개 log-spaced 나이 버킷 (5m~15d) | 5min |
| WriteProber | 자체 blob 제출 (optional, ETH 소모) | 5min |
| StakeIndexer | HHI 계산, quorum별 stake 스냅샷 | 1h |
| EjectionIndexer | OperatorEjected eth_getLogs | 12s |

## Threat Model Mapping

| Category | Metrics |
|---|---|
| A. Promise Breach | Survival curve, blob recoverability |
| B. Trust Outsourcing | Relay vs operator success divergence |
| C. Authority Concentration | HHI per quorum, top-5 stake share |
| D. Verification Gaps | KZG failure rate, attestation vs retrievability |
| E. Economic Assumptions | Ejection frequency, dead operator ratio |
| F. Version Gaps | Operator v2 connection errors |

## Quick Start

```bash
# 1. Copy env
cp .env.example .env
# Edit .env: set ETH_RPC_URL (Alchemy/Infura mainnet)

# 2. Start (TimescaleDB + Prober + Grafana)
docker compose up -d

# 3. Access
# Grafana: http://localhost:3000 (admin/admin)
# API:     http://localhost:8080/healthz
```

## API Endpoints

| Endpoint | Description |
|---|---|
| `GET /healthz` | Health check |
| `GET /api/status` | System status (1h relay/operator rates) |
| `GET /api/survival` | Survival curve with Wilson 95% CI |
| `GET /api/operators` | Per-operator stats + dead operators |
| `GET /api/relays` | Per-relay success rates |
| `GET /api/hhi?hours=168` | HHI time series per quorum |
| `GET /api/ejections?limit=100` | Recent ejection events |
| `GET /api/probes?limit=50` | Recent probe log |

## GCP Deployment

```bash
# Build and push
docker build -t bonda-eigenda-prober .
# Deploy to GCP (34.47.125.58)
# Connect to existing TimescaleDB + Grafana
```

## Tech Stack

- Go 1.26, single binary
- TimescaleDB (hypertables + continuous aggregates)
- Grafana (threat model dashboards)
- gRPC (EigenDA relay + operator + disperser)
- EigenDA DataAPI v2 (REST)
