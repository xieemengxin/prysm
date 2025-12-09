package stateutil

import (
	fieldparams "github.com/OffchainLabs/prysm/v6/config/fieldparams"
	"github.com/OffchainLabs/prysm/v6/encoding/ssz"
	ethpb "github.com/OffchainLabs/prysm/v6/proto/prysm/v1alpha1"
)

// TermDepositsRoot computes the SSZ hash tree root of term deposits slice.
func TermDepositsRoot(slice []*ethpb.TermDeposit) ([32]byte, error) {
	return ssz.SliceRoot(slice, fieldparams.TermDepositsLimit)
}
