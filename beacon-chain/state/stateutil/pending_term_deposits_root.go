package stateutil

import (
	fieldparams "github.com/OffchainLabs/prysm/v6/config/fieldparams"
	"github.com/OffchainLabs/prysm/v6/encoding/ssz"
	ethpb "github.com/OffchainLabs/prysm/v6/proto/prysm/v1alpha1"
)

// PendingTermDepositsRoot computes the SSZ hash tree root of pending term deposits slice.
func PendingTermDepositsRoot(slice []*ethpb.PendingTermDeposit) ([32]byte, error) {
	return ssz.SliceRoot(slice, fieldparams.PendingTermDepositsLimit)
}
