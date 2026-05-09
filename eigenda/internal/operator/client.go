package operator

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/jyo-o/BONDA/eigenda/internal/operator/validatorpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type ChunkProbeResult struct {
	Success        bool
	LatencyMs      int
	ChunksReturned int
	Error          string
}

type Client struct {
	timeout time.Duration
}

func NewClient() *Client {
	return &Client{
		timeout: 5 * time.Second,
	}
}

// ProbeChunks retrieves chunks directly from a v2 operator node using
// validator.Retrieval.GetChunks(blob_key, quorum_id) on the v2 retrieval port.
func (c *Client) ProbeChunks(ctx context.Context, operatorSocket string, blobKeyHex string, quorumID uint32) *ChunkProbeResult {
	start := time.Now()
	retrievalSocket := ParseV2RetrievalSocket(operatorSocket)

	blobKeyBytes, err := hex.DecodeString(strings.TrimPrefix(blobKeyHex, "0x"))
	if err != nil {
		return &ChunkProbeResult{Success: false, Error: fmt.Sprintf("decode blob key: %v", err)}
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	conn, err := grpc.DialContext(ctx, retrievalSocket,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return &ChunkProbeResult{
			Success:   false,
			LatencyMs: int(time.Since(start).Milliseconds()),
			Error:     fmt.Sprintf("dial %s: %v", retrievalSocket, err),
		}
	}
	defer conn.Close()

	client := validatorpb.NewRetrievalClient(conn)
	resp, err := client.GetChunks(ctx, &validatorpb.GetChunksRequest{
		BlobKey:  blobKeyBytes,
		QuorumId: quorumID,
	})

	latencyMs := int(time.Since(start).Milliseconds())
	if err != nil {
		return &ChunkProbeResult{Success: false, LatencyMs: latencyMs, Error: fmt.Sprintf("GetChunks: %v", err)}
	}

	return &ChunkProbeResult{
		Success:        true,
		LatencyMs:      latencyMs,
		ChunksReturned: len(resp.GetChunks()),
	}
}
