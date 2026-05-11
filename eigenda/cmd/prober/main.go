package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/joho/godotenv"

	"github.com/jyo-o/BONDA/eigenda/internal/api"
	"github.com/jyo-o/BONDA/eigenda/internal/config"
	"github.com/jyo-o/BONDA/eigenda/internal/contracts"
	"github.com/jyo-o/BONDA/eigenda/internal/dataapi"
	"github.com/jyo-o/BONDA/eigenda/internal/db"
	"github.com/jyo-o/BONDA/eigenda/internal/eigenexplorer"
	"github.com/jyo-o/BONDA/eigenda/internal/kzg"
	"github.com/jyo-o/BONDA/eigenda/internal/operator"
	"github.com/jyo-o/BONDA/eigenda/internal/registry"
	"github.com/jyo-o/BONDA/eigenda/internal/relay"
	"github.com/jyo-o/BONDA/eigenda/internal/worker"
)

func main() {
	if err := godotenv.Load(); err != nil {
		_ = godotenv.Load("../.env")
	}

	cfg := config.Load()

	log.Println("starting BONDA EigenDA mainnet monitor")
	log.Printf("dataapi: %s", cfg.DataAPIBaseURL)

	// Database
	database, err := db.New(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer database.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := database.RunMigrations(ctx); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}
	log.Println("database migrations complete")

	// DataAPI client
	apiClient := dataapi.NewClient(cfg.DataAPIBaseURL)

	// Shared ETH RPC client for on-chain workers
	var ethClient *ethclient.Client
	var dirAddr common.Address
	if cfg.EthRPCURL != "" && cfg.EigenDADirectory != "" {
		ethClient, err = ethclient.Dial(cfg.EthRPCURL)
		if err != nil {
			log.Printf("WARNING: failed to connect to ETH RPC: %v", err)
		} else {
			dirAddr = common.HexToAddress(cfg.EigenDADirectory)
			log.Printf("ETH RPC connected, directory=%s", dirAddr.Hex())
		}
	}

	// Relay registry (requires ETH RPC)
	var reg *registry.RelayRegistry
	if cfg.EthRPCURL != "" && cfg.EigenDADirectory != "" {
		reg, err = registry.NewFromDirectory(cfg.EthRPCURL, cfg.EigenDADirectory)
		if err != nil {
			log.Fatalf("failed to resolve relay registry: %v", err)
		}
		defer reg.Close()
		log.Printf("resolved EigenDARelayRegistry at %s", reg.RegistryAddress().Hex())
	} else {
		log.Println("WARNING: ETH_RPC_URL or EIGENDA_DIRECTORY not set, relay probing disabled")
	}

	// Relay client
	relayClient := relay.NewClient()

	// Operator discovery + client
	var opDiscovery *operator.Discovery
	var opClient *operator.Client
	if (cfg.OperatorVerifierEnabled || cfg.StakeIndexerEnabled) && cfg.EthRPCURL != "" && cfg.EigenDADirectory != "" {
		opDiscovery, err = operator.NewDiscovery(cfg.EthRPCURL, cfg.EigenDADirectory)
		if err != nil {
			log.Printf("WARNING: operator discovery failed: %v", err)
		} else {
			defer opDiscovery.Close()
			opClient = operator.NewClient()
			log.Println("operator discovery enabled")
		}
	}

	// Worker manager
	mgr := worker.NewManager()

	if cfg.CollectorEnabled {
		mgr.Register(worker.NewBlobCollector(apiClient, database, cfg.CollectorPollInterval))
	}

	// KZG verifier
	kzgVerifier := kzg.NewVerifier(cfg.KZGVerifyEnabled)

	if cfg.RelayVerifierEnabled && reg != nil {
		mgr.Register(worker.NewRelayVerifier(apiClient, database, relayClient, reg, kzgVerifier, cfg.RelayVerifierParallel))
	}

	if cfg.OperatorVerifierEnabled && opDiscovery != nil && opClient != nil {
		mgr.Register(worker.NewOperatorVerifier(database, opDiscovery, opClient))
	}

	if cfg.ReverifierEnabled && reg != nil {
		mgr.Register(worker.NewReverifier(apiClient, database, relayClient, reg, cfg.ReverifierInterval))
	}

	// Stake indexer (requires ETH RPC + operator discovery)
	if cfg.StakeIndexerEnabled && ethClient != nil && opDiscovery != nil {
		mgr.Register(worker.NewStakeIndexer(database, opDiscovery, ethClient, dirAddr, cfg.StakeIndexerInterval))
	}

	// Ejection indexer (requires ETH RPC)
	if cfg.EjectionIndexerEnabled && ethClient != nil {
		mgr.Register(worker.NewEjectionIndexer(database, ethClient, dirAddr, cfg.EjectionPollInterval))
	}

	// Write prober (optional, costs ETH)
	if cfg.WriteProberEnabled {
		mgr.Register(worker.NewWriteProber(database, apiClient, cfg.EigenDAProxyURL, cfg.DisperserAccountID, cfg.WriteProberInterval))
	}

	// Operator status — resolves metadata from on-chain contracts (no API key needed)
	if cfg.OperatorStatusEnabled && ethClient != nil && opDiscovery != nil {
		regCoordAddr, err := contracts.ResolveAddress(ctx, ethClient, dirAddr, "REGISTRY_COORDINATOR")
		if err != nil {
			log.Printf("WARNING: failed to resolve REGISTRY_COORDINATOR for operator-status: %v", err)
		} else {
			metaClient, err := eigenexplorer.NewClient(ethClient, regCoordAddr)
			if err != nil {
				log.Printf("WARNING: failed to create metadata client: %v", err)
			} else {
				mgr.Register(worker.NewOperatorStatusWorker(database, metaClient, opDiscovery, cfg.OperatorStatusInterval))
			}
		}
	}

	// Health recorder
	if cfg.HealthRecorderEnabled {
		mgr.Register(worker.NewHealthRecorder(database, cfg.HealthRecorderInterval))
	}

	// API server
	apiServer := api.NewServer(database.Conn(), cfg.APIListenAddr)
	go func() {
		if err := apiServer.Start(); err != nil && err != http.ErrServerClosed {
			log.Printf("API server error: %v", err)
		}
	}()

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.Printf("received %s, shutting down", sig)
		_ = apiServer.Shutdown(ctx)
		cancel()
	}()

	mgr.Run(ctx)
}
