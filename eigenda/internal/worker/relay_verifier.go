package worker

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jyo-o/BONDA/eigenda/internal/dataapi"
	"github.com/jyo-o/BONDA/eigenda/internal/db"
	"github.com/jyo-o/BONDA/eigenda/internal/kzg"
	"github.com/jyo-o/BONDA/eigenda/internal/registry"
	"github.com/jyo-o/BONDA/eigenda/internal/relay"
)

type RelayVerifier struct {
	api      *dataapi.Client
	db       *db.DB
	relay    *relay.Client
	registry *registry.RelayRegistry
	verifier *kzg.Verifier
	parallel int
}

func NewRelayVerifier(api *dataapi.Client, database *db.DB, relayClient *relay.Client,
	reg *registry.RelayRegistry, verifier *kzg.Verifier, parallel int) *RelayVerifier {
	return &RelayVerifier{
		api:      api,
		db:       database,
		relay:    relayClient,
		registry: reg,
		verifier: verifier,
		parallel: parallel,
	}
}

func (v *RelayVerifier) Name() string { return "relay-verifier" }

func (v *RelayVerifier) Run(ctx context.Context) {
	log.Println("[relay-verifier] started")
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		unprobed, err := v.db.GetUnprobedBlobs(ctx, 20)
		if err != nil || len(unprobed) == 0 {
			time.Sleep(2 * time.Second)
			continue
		}

		var wg sync.WaitGroup
		sem := make(chan struct{}, v.parallel)
		for _, blob := range unprobed {
			wg.Add(1)
			go func(bk string, ra uint64) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				v.probeBlob(ctx, bk, ra)
				v.fetchAttestation(ctx, bk)
			}(blob.BlobKey, blob.RequestedAt)
		}
		wg.Wait()
	}
}

func (v *RelayVerifier) probeBlob(ctx context.Context, blobKey string, requestedAt uint64) {
	if v.registry == nil {
		return
	}
	blobAgeHours := float64(time.Now().UnixNano()-int64(requestedAt)) / float64(time.Hour)

	cert, err := v.api.FetchCertificate(blobKey)
	if err != nil {
		if strings.Contains(err.Error(), "HTTP 500") && strings.Contains(err.Error(), "certificate not found") {
			return
		}
		v.db.InsertProbeResult(ctx, &db.ProbeResult{
			BlobKey: blobKey, BlobAgeHours: blobAgeHours,
			RelayKey: -1, Success: false, ErrorMessage: err.Error(),
		})
		return
	}
	if len(cert.BlobCertificate.RelayKeys) == 0 {
		return
	}
	for _, relayKey := range cert.BlobCertificate.RelayKeys {
		v.probeRelay(ctx, blobKey, blobAgeHours, relayKey)
	}
}

func (v *RelayVerifier) probeRelay(ctx context.Context, blobKey string, blobAgeHours float64, relayKey uint32) {
	relayURL, err := v.registry.GetRelayURL(ctx, relayKey)
	if err != nil {
		v.db.InsertProbeResult(ctx, &db.ProbeResult{
			BlobKey: blobKey, BlobAgeHours: blobAgeHours,
			RelayKey: int(relayKey), Success: false,
			ErrorMessage: fmt.Sprintf("registry lookup: %v", err),
		})
		return
	}
	result := v.relay.GetBlob(ctx, relayURL, blobKey)

	probe := &db.ProbeResult{
		BlobKey: blobKey, BlobAgeHours: blobAgeHours,
		RelayKey: int(relayKey), Success: result.Success,
		LatencyMs: result.LatencyMs, ErrorMessage: result.Error,
		DataSizeBytes: result.DataSizeBytes,
	}

	// KZG verification on successful retrieval
	if result.Success && v.verifier != nil && len(result.Data) > 0 {
		commitX, commitY, err := v.db.GetBlobCommitment(ctx, blobKey)
		if err == nil {
			verified, kzgErr := v.verifier.VerifyBlob(result.Data, commitX, commitY)
			probe.KZGVerified = &verified
			probe.KZGError = kzgErr
		}
	}

	v.db.InsertProbeResult(ctx, probe)

	status := "OK"
	if !result.Success {
		status = "FAIL"
	}
	logKey := blobKey
	if len(logKey) > 12 {
		logKey = logKey[:12]
	}
	kzgTag := ""
	if probe.KZGVerified != nil && !*probe.KZGVerified {
		kzgTag = " KZG_FAIL"
	}
	log.Printf("[relay] %s blob=%s relay=%d latency=%dms%s", status, logKey, relayKey, result.LatencyMs, kzgTag)
}

func (v *RelayVerifier) fetchAttestation(ctx context.Context, blobKey string) {
	att, err := v.api.FetchAttestationInfo(blobKey)
	if err != nil {
		return
	}
	nonSignerCount := len(att.AttestationInfo.Attestation.NonSignerPubKeys)
	for qStr, signingPct := range att.AttestationInfo.Attestation.QuorumResults {
		qNum, _ := strconv.Atoi(qStr)
		v.db.InsertAttestation(ctx, &db.AttestationSnapshot{
			BlobKey: blobKey, QuorumNumber: qNum,
			TotalNonSigners: nonSignerCount, SigningStakePercentage: float64(signingPct),
		})
	}
}
