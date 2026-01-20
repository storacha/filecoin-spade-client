package spadeclient

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"filecoin-spade-client/pkg/config"
	"filecoin-spade-client/pkg/log"
	"filecoin-spade-client/pkg/lotusclient"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	fildatasegment "github.com/storacha/fil-datasegment/pkg/dlass"
	"golang.org/x/xerrors"
)

type SpadeClient struct {
	Config        config.SpadeConfig
	LotusClient   *lotusclient.LotusClient
	HttpTransport http.RoundTripper

	LatestEligiblePiecesRequest       PiecesEligibleResponseEnvelope
	LatestEligiblePiecesRequestMoment time.Time

	requestedPieces      []string
	requestedPiecesMutex sync.Mutex
}

func New(config config.Configuration, client *lotusclient.LotusClient) *SpadeClient {
	sc := new(SpadeClient)
	sc.Config = config.SpadeConfig
	sc.LotusClient = client
	sc.HttpTransport = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: config.InsecureSkipVerify},
	}
	sc.LatestEligiblePiecesRequestMoment = time.Now().Add(-time.Hour)
	return sc
}

func (sc *SpadeClient) AddRequestedPiece(pieceCid string) {
	sc.requestedPiecesMutex.Lock()
	sc.requestedPieces = append(sc.requestedPieces, pieceCid)
	sc.requestedPiecesMutex.Unlock()
}

func (sc *SpadeClient) Start(ctx context.Context) {
	_, err := sc.PendingProposals(ctx)
	if err != nil {
		log.Fatalf("error starting spade client: %+v", err)
	}
	log.Infof("Successfully connected to Spade API")

	go func() {
		select {
		case <-ctx.Done():
			log.Infof("shutting down spade API client: context done")

			return
		}
	}()
}

func (sc *SpadeClient) PendingProposals(ctx context.Context) (*ResponsePendingProposals, error) {
	data, err := sc.doRequest(ctx, "GET", "/sp/pending_proposals", "")
	if err != nil {
		return nil, xerrors.Errorf("error checking pending proposals: %+v", err)
	}

	var resp PendingProposalResponseEnvelope
	err = json.Unmarshal(data, &resp)
	if err != nil {
		return nil, xerrors.Errorf("could not unmarshall response: %+v", err)
	}

	return &resp.Response, nil
}

func (sc *SpadeClient) RequestNewDeal(ctx context.Context) (string, error) {
	// We add some cache to this request because this can happen often
	cached := true                                                          // small display hack
	if time.Now().Unix()-sc.LatestEligiblePiecesRequestMoment.Unix() > 10 { // 10 second cache
		data, err := sc.doRequest(ctx, "GET", "/sp/eligible_pieces", "")
		if err != nil {
			return "", xerrors.Errorf("error checking eligible pieces: %+v", err)
		}

		//log.Debugf("PENDING RESPONSE: %s", data)

		sc.LatestEligiblePiecesRequestMoment = time.Now()
		err = json.Unmarshal(data, &sc.LatestEligiblePiecesRequest)
		if err != nil {
			return "", xerrors.Errorf("could not unmarshall response: %+v", err)
		}
		cached = false
	}

	resp := sc.LatestEligiblePiecesRequest

	if *resp.ResponseEntries == 0 {
		if !cached {
			log.Infof(" > No eligible pieces at the moment.")
		}
		return "", nil
	}

	if !cached {
		log.Infof(" > Found %d eligible pieces", *resp.ResponseEntries)
	}

	// find one that we do not already have requested
	for _, piece := range resp.Response {
		if !sc.hasRequestedPiece(piece.PieceCid) {
			log.Infof("  > Requesting %s", piece.PieceCid)

			_, err := sc.invoke(ctx, piece.PieceCid, piece.TenantPolicyCid)
			if err != nil {
				if err.Error() == "ErrTooManyReplicas" {
					// If its "overreplicated" we can just add the piece to our requested pieces - we'll ignore it next run
					sc.AddRequestedPiece(piece.PieceCid)
				}
				return "", xerrors.Errorf("   > Could not invoke reservation %s: %s", piece.PieceCid, err)
			}

			sc.AddRequestedPiece(piece.PieceCid)
			log.Infof("   > Successfully requested %s", piece.PieceCid)
			return piece.PieceCid, nil
		}
	}

	return "", xerrors.New(" > no eligible pieces are valid to be requested")
}

func (sc *SpadeClient) invoke(ctx context.Context, pid string, policycid string) (*ResponseDealRequest, error) {
	data, err := sc.doRequest(
		ctx,
		"POST",
		"/sp/invoke",
		fmt.Sprintf("call=reserve_piece&piece_cid=%s&tenant_policy=%s", pid, policycid),
	)
	if err != nil {
		return nil, xerrors.Errorf("error invoking reservation request: %+v", err)
	}

	//log.Infof("Invoke response:\n %s", string(data))

	//@todo map this data?
	var resp DealRequestResponseEnvelope
	err = json.Unmarshal(data, &resp)
	if err != nil {
		return nil, xerrors.Errorf("could not unmarshall response: %+v", err)
	}

	if resp.ErrSlug != "" {
		return nil, xerrors.New(resp.ErrSlug)
	}

	return &resp.Response, nil
}

func (sc *SpadeClient) RequestPieceManifest(ctx context.Context, proposalId string) (*fildatasegment.Agg, error) {
	data, err := sc.doRequest(
		ctx,
		"GET",
		fmt.Sprintf("/sp/piece_manifest?proposal=%s", proposalId),
		"",
	)

	if err != nil {
		return nil, xerrors.Errorf("error requesting piece manifest: %+v", err)
	}

	var resp PieceManifestResponseEnvelope
	err = json.Unmarshal(data, &resp)
	if err != nil {
		return nil, xerrors.Errorf("could not unmarshall response: %+v", err)
	}

	return &resp.Response, nil
}

func (sc *SpadeClient) hasRequestedPiece(pid string) bool {
	sc.requestedPiecesMutex.Lock()
	defer sc.requestedPiecesMutex.Unlock()
	for _, v := range sc.requestedPieces {
		if v == pid {
			return true
		}
	}
	return false
}

func (sc *SpadeClient) doRequest(ctx context.Context, method string, url string, authPrefix string) ([]byte, error) {
	req, _ := http.NewRequest(method, sc.Config.Url+url, strings.NewReader(""))
	req.Header.Set("Authorization", sc.LotusClient.GetSpadeAuthSignature(ctx, authPrefix))

	resp, err := sc.HttpTransport.RoundTrip(req)
	if err != nil {
		return []byte{}, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return []byte{}, xerrors.New(fmt.Sprintf("could not read spade response: %s", err))
	}

	if resp.StatusCode == 401 {
		return body, xerrors.New("spade API returned 401 (Unauthorized) - wrong token?")
	}

	if resp.StatusCode != 200 {
		log.Debugf("spade returned response body: %s", body)
		return body, xerrors.New(fmt.Sprintf("spade API returned %d instead of expected 200", resp.StatusCode))
	}

	return body, nil
}
