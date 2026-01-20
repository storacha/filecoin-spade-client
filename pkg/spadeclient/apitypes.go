package spadeclient

import (
	"time"

	fildatasegment "github.com/storacha/fil-datasegment/pkg/dlass"
)

//go:generate go run golang.org/x/tools/cmd/stringer -type=APIErrorCode -output=types_err.go

//nolint:revive
type APIErrorCode int

//nolint:revive
const (
	ErrInvalidRequest            APIErrorCode = 4400
	ErrUnauthorizedAccess        APIErrorCode = 4401
	ErrSystemTemporarilyDisabled APIErrorCode = 4503
)

// ResponseEnvelope is the structure wrapping all responses from the deal engine
type ResponseEnvelope[T ResponsePayload] struct {
	RequestID          string    `json:"request_id,omitempty"`
	ResponseTime       time.Time `json:"response_timestamp"`
	ResponseStateEpoch int64     `json:"response_state_epoch,omitempty"`
	ResponseCode       int       `json:"response_code"`
	ErrCode            int       `json:"error_code,omitempty"`
	ErrSlug            string    `json:"error_slug,omitempty"`
	ErrLines           []string  `json:"error_lines,omitempty"`
	InfoLines          []string  `json:"info_lines,omitempty"`
	ResponseEntries    *int      `json:"response_entries,omitempty"`
	Response           T         `json:"response"`
}

//nolint:revive
type ResponsePayload interface{}

const (
	cidTrimPrefix = 6
	cidTrimSuffix = 8
)

//nolint:revive
func TrimCidString(cs string) string {
	if len(cs) <= cidTrimPrefix+cidTrimSuffix+2 {
		return cs
	}
	return cs[0:cidTrimPrefix] + "~" + cs[len(cs)-cidTrimSuffix:]
}

//nolint:revive
const (
	ErrOversizedPiece                  APIErrorCode = 4011
	ErrStorageProviderSuspended        APIErrorCode = 4012
	ErrStorageProviderIneligibleToMine APIErrorCode = 4013

	ErrStorageProviderInfoTooOld  APIErrorCode = 4041
	ErrStorageProviderUndialable  APIErrorCode = 4042
	ErrStorageProviderUnsupported APIErrorCode = 4043

	ErrUnclaimedPieceCID         APIErrorCode = 4020
	ErrProviderHasReplica        APIErrorCode = 4021
	ErrTenantsOutOfDatacap       APIErrorCode = 4022
	ErrTooManyReplicas           APIErrorCode = 4023
	ErrProviderAboveMaxInFlight  APIErrorCode = 4024
	ErrReplicationRulesViolation APIErrorCode = 4029 // catch-all for when there is no common rejection theme for competing tenants

	ErrExternalReservationRefused APIErrorCode = 4030 // some tenants are looking to add an additional check on their end
	ErrSPUnsupported              APIErrorCode = 4044
)

// ResponsePendingProposals is the response payload returned by the .../sp/pending_proposals endpoint
type ResponsePendingProposals struct {
	RecentFailures   []ProposalFailure `json:"recent_failures,omitempty"`
	PendingProposals []DealProposal    `json:"pending_proposals"`
}

// ResponseDealRequest is the response payload returned by the .../sp/request_piece/{{PieceCid}} endpoint
type ResponseDealRequest struct {
	ReplicationStates []TenantReplicationState `json:"tenant_replication_states"`
	DealStartTime     *time.Time               `json:"deal_start_time,omitempty"`
	DealStartEpoch    *int64                   `json:"deal_start_epoch,omitempty"`
}

// ResponsePiecesEligible is the response payload returned by the .../sp/eligible_pieces endpoint
type ResponsePiecesEligible []*Piece

//nolint:revive
type SPInfo struct {
	Errors             []string            `json:"errors,omitempty"`
	SectorLog2Size     uint8               `json:"sector_log2_size"`
	PeerID             *string             `json:"peerid"`
	MultiAddrs         []string            `json:"multiaddrs"`
	RetrievalProtocols map[string][]string `json:"retrieval_protocols,omitempty"`
	PeerInfo           *struct {
		Protos map[string]struct{}    `json:"libp2p_protocols"`
		Meta   map[string]interface{} `json:"meta"`
	} `json:"peer_info,omitempty"`
}

//nolint:revive
type ProposalFailure struct {
	ErrorTimeStamp time.Time `json:"timestamp"`
	Error          string    `json:"error"`
	PieceCid       string    `json:"piece_cid"`
	ProposalID     string    `json:"deal_proposal_id"`
	ProposalCid    *string   `json:"deal_proposal_cid,omitempty"`
	TenantID       int16     `json:"tenant_id"`
	TenantClient   string    `json:"tenant_client_id"`
}

//nolint:revive
type DealProposal struct {
	ProposalID     string    `json:"deal_proposal_id"`
	ProposalCid    *string   `json:"deal_proposal_cid,omitempty"`
	HoursRemaining int       `json:"hours_remaining"`
	PieceSize      int64     `json:"piece_size"`
	PieceCid       string    `json:"piece_cid"`
	TenantID       int16     `json:"tenant_id"`
	TenantClient   string    `json:"tenant_client_id"`
	StartTime      time.Time `json:"deal_start_time"`
	StartEpoch     int64     `json:"deal_start_epoch"`
	ImportCmd      string    `json:"sample_import_cmd"`
	Segmentation   *string   `json:"segmentation_type,omitempty"`
	AssemblyCmd    *string   `json:"sample_assembly_cmd,omitempty"`
	DataSources    []string  `json:"data_sources,omitempty"`
}

//nolint:revive
type TenantReplicationState struct {
	TenantID     int16   `json:"tenant_id"`
	TenantClient *string `json:"tenant_client_id"`

	MaxInFlightBytes int64 `json:"tenant_max_in_flight_bytes"`
	SpInFlightBytes  int64 `json:"actual_in_flight_bytes" db:"cur_in_flight_bytes"`

	MaxTotal     int16 `json:"tenant_max_total"`
	MaxOrg       int16 `json:"tenant_max_per_org"         db:"max_per_org"`
	MaxCity      int16 `json:"tenant_max_per_metro"       db:"max_per_city"`
	MaxCountry   int16 `json:"tenant_max_per_country"     db:"max_per_country"`
	MaxContinent int16 `json:"tenant_max_per_continent"   db:"max_per_continent"`

	Total       int16 `json:"actual_total"                db:"cur_total"`
	InOrg       int16 `json:"actual_within_org"           db:"cur_in_org"`
	InCity      int16 `json:"actual_within_metro"         db:"cur_in_city"`
	InCountry   int16 `json:"actual_within_country"       db:"cur_in_country"`
	InContinent int16 `json:"actual_within_continent"     db:"cur_in_continent"`

	DealAlreadyExists bool `json:"sp_holds_qualifying_deal"`
}

//nolint:revive
type Piece struct {
	PieceCid         string `json:"piece_cid"`
	PaddedPieceSize  uint64 `json:"padded_piece_size"`
	ClaimingTenant   int16  `json:"tenant_id"`
	TenantPolicyCid  string `json:"tenant_policy_cid"`
	SampleReserveCmd string `json:"sample_reserve_cmd,omitempty"`
}

/// These are custom types because the apitypes isn't reflective of the actual API

// PendingProposalResponseEnvelope is the response envelope for pending proposals
type PendingProposalResponseEnvelope = ResponseEnvelope[ResponsePendingProposals]

// PiecesEligibleResponseEnvelope is the response envelope for eligible pieces
type PiecesEligibleResponseEnvelope = ResponseEnvelope[ResponsePiecesEligible]

// DealRequestResponseEnvelope is the response envelope for invoking a new deal request
type DealRequestResponseEnvelope = ResponseEnvelope[ResponseDealRequest]

// PieceManifestResponseEnvelope is the response envelope for piece manifest requests
type PieceManifestResponseEnvelope = ResponseEnvelope[fildatasegment.Agg]
