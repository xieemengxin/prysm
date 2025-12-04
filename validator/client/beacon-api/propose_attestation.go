package beacon_api

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/OffchainLabs/prysm/v7/beacon-chain/core/helpers"
	ethpb "github.com/OffchainLabs/prysm/v7/proto/prysm/v1alpha1"
	"github.com/OffchainLabs/prysm/v7/runtime/version"
	"github.com/OffchainLabs/prysm/v7/time/slots"
	"github.com/pkg/errors"
)

func (c *beaconApiValidatorClient) submitAttestations(ctx context.Context, atts []*ethpb.Attestation) ([]*ethpb.AttestResponse, error) {
	if len(atts) == 0 {
		return nil, nil
	}
	for _, a := range atts {
		if err := helpers.ValidateNilAttestation(a); err != nil {
			return nil, err
		}
	}

	marshalledAtts, err := json.Marshal(jsonifyAttestations(atts))
	if err != nil {
		return nil, err
	}

	consensusVersion := version.String(slots.ToForkVersion(atts[0].Data.Slot))
	headers := map[string]string{"Eth-Consensus-Version": consensusVersion}
	err = c.jsonRestHandler.Post(
		ctx,
		"/eth/v2/beacon/pool/attestations",
		headers,
		bytes.NewBuffer(marshalledAtts),
		nil,
	)
	if err != nil {
		return nil, err
	}

	resp := make([]*ethpb.AttestResponse, len(atts))
	for i, a := range atts {
		// TODO: can this be different for each att?
		attestationDataRoot, err := a.Data.HashTreeRoot()
		if err != nil {
			return nil, errors.Wrap(err, "failed to compute attestation data root")
		}
		resp[i] = &ethpb.AttestResponse{AttestationDataRoot: attestationDataRoot[:]}
	}
	return resp, nil
}

func (c *beaconApiValidatorClient) submitAttestationsElectra(ctx context.Context, atts []*ethpb.SingleAttestation) ([]*ethpb.AttestResponse, error) {
	if len(atts) == 0 {
		return nil, nil
	}
	for _, a := range atts {
		if err := helpers.ValidateNilAttestation(a); err != nil {
			return nil, err
		}
	}

	marshalledAttestation, err := json.Marshal(jsonifySingleAttestations(atts))
	if err != nil {
		return nil, err
	}

	consensusVersion := version.String(slots.ToForkVersion(atts[0].Data.Slot))
	headers := map[string]string{"Eth-Consensus-Version": consensusVersion}
	if err = c.jsonRestHandler.Post(
		ctx,
		"/eth/v2/beacon/pool/attestations",
		headers,
		bytes.NewBuffer(marshalledAttestation),
		nil,
	); err != nil {
		return nil, err
	}

	resp := make([]*ethpb.AttestResponse, len(atts))
	for i, a := range atts {
		// TODO: can this be different for each att?
		attestationDataRoot, err := a.Data.HashTreeRoot()
		if err != nil {
			return nil, errors.Wrap(err, "failed to compute attestation data root")
		}
		resp[i] = &ethpb.AttestResponse{AttestationDataRoot: attestationDataRoot[:]}
	}
	return resp, nil
}
