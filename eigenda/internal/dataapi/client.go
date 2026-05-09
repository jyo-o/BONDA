package dataapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// --- Response types ---

type BlobCommitment struct {
	X string `json:"X"`
	Y string `json:"Y"`
}

type BlobCommitments struct {
	Commitment BlobCommitment `json:"commitment"`
	Length     int            `json:"length"`
}

type PaymentMetadata struct {
	AccountID         string `json:"account_id"`
	Timestamp         uint64 `json:"timestamp"`
	CumulativePayment int    `json:"cumulative_payment"`
}

type BlobHeader struct {
	BlobVersion     int             `json:"BlobVersion"`
	BlobCommitments BlobCommitments `json:"BlobCommitments"`
	QuorumNumbers   string          `json:"QuorumNumbers"`
	PaymentMetadata PaymentMetadata `json:"PaymentMetadata"`
}

type BlobMetadata struct {
	BlobHeader    BlobHeader `json:"blob_header"`
	BlobStatus    string     `json:"blob_status"`
	BlobSizeBytes int        `json:"blob_size_bytes"`
	RequestedAt   uint64     `json:"requested_at"`
	ExpiryUnixSec uint64     `json:"expiry_unix_sec"`
}

type BlobEntry struct {
	BlobKey      string       `json:"blob_key"`
	BlobMetadata BlobMetadata `json:"blob_metadata"`
}

type FeedResponse struct {
	Blobs  []BlobEntry `json:"blobs"`
	Cursor string      `json:"cursor"`
}

type RelayInfo struct {
	RelayKey uint32 `json:"relay_key"`
}

type BlobCertificate struct {
	BlobHeader BlobHeader `json:"BlobHeader"`
	Signature  string     `json:"Signature"`
	RelayKeys  []uint32   `json:"RelayKeys"`
}

type CertificateResponse struct {
	BlobCertificate BlobCertificate `json:"blob_certificate"`
}

type Attestation struct {
	QuorumNumbers    string            `json:"QuorumNumbers"`
	QuorumResults    map[string]int    `json:"QuorumResults"`
	NonSignerPubKeys []interface{}     `json:"NonSignerPubKeys"`
}

type BlobInclusionInfo struct {
	BatchHeader struct {
		BatchRoot            string `json:"batch_root"`
		ReferenceBlockNumber int    `json:"reference_block_number"`
	} `json:"batch_header"`
	BlobIndex      int    `json:"blob_index"`
	InclusionProof string `json:"inclusion_proof"`
}

type AttestationInfo struct {
	Attestation Attestation `json:"attestation"`
}

type AttestationResponse struct {
	BlobKey           string            `json:"blob_key"`
	BatchHeaderHash   string            `json:"batch_header_hash"`
	BlobInclusionInfo BlobInclusionInfo `json:"blob_inclusion_info"`
	AttestationInfo   AttestationInfo   `json:"attestation_info"`
}

// --- Client ---

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) FetchBlobFeed(limit int, cursor string) (*FeedResponse, error) {
	params := url.Values{}
	params.Set("direction", "backward")
	params.Set("limit", fmt.Sprintf("%d", limit))
	if cursor != "" {
		params.Set("cursor", cursor)
	}

	reqURL := fmt.Sprintf("%s/blobs/feed?%s", c.baseURL, params.Encode())

	resp, err := c.doGet(reqURL)
	if err != nil {
		return nil, fmt.Errorf("fetch blob feed: %w", err)
	}

	var feed FeedResponse
	if err := json.Unmarshal(resp, &feed); err != nil {
		return nil, fmt.Errorf("decode blob feed: %w", err)
	}
	return &feed, nil
}

func (c *Client) FetchCertificate(blobKey string) (*CertificateResponse, error) {
	reqURL := fmt.Sprintf("%s/blobs/%s/certificate", c.baseURL, blobKey)

	resp, err := c.doGet(reqURL)
	if err != nil {
		return nil, fmt.Errorf("fetch certificate for %s: %w", blobKey, err)
	}

	var cert CertificateResponse
	if err := json.Unmarshal(resp, &cert); err != nil {
		return nil, fmt.Errorf("decode certificate for %s: %w", blobKey, err)
	}
	return &cert, nil
}

func (c *Client) FetchAttestationInfo(blobKey string) (*AttestationResponse, error) {
	reqURL := fmt.Sprintf("%s/blobs/%s/attestation-info", c.baseURL, blobKey)

	resp, err := c.doGet(reqURL)
	if err != nil {
		return nil, fmt.Errorf("fetch attestation for %s: %w", blobKey, err)
	}

	var att AttestationResponse
	if err := json.Unmarshal(resp, &att); err != nil {
		return nil, fmt.Errorf("decode attestation for %s: %w", blobKey, err)
	}
	return &att, nil
}

func (c *Client) doGet(url string) ([]byte, error) {
	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode == 429 {
			backoff := time.Duration(1<<uint(i)) * time.Second
			time.Sleep(backoff)
			continue
		}

		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
		}

		return io.ReadAll(resp.Body)
	}
	return nil, fmt.Errorf("max retries exceeded for %s", url)
}
