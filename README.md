# BONDA

**Blockchain Operator Network Data Analytics**

4개 DA(Data Availability) provider의 안전성과 현황을 실시간으로 보여주는 대시보드.
EigenDA 위협 모델링에서 출발하여, Ethereum DA / Celestia / Avail까지 확장.

## Project Structure
```
BONDA/
├── eigenda/        # EigenDA mainnet monitor (Go, 7 workers)
├── celestia/       # (planned)
├── avail/          # (planned)
└── ethereum/       # (planned)
```

## EigenDA Module
See [eigenda/README.md](eigenda/README.md) for details.

## Infrastructure
- GCP: 34.47.125.58 (Grafana + TimescaleDB)
- Celestia & Avail light nodes: operational
- Ethereum full node: planned

## Timeline
- Mid-presentation: 2026-05-22
- Final presentation: 2026-06-19
