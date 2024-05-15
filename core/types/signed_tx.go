package types

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/holiman/uint256"
	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/common/hexutil"
	"github.com/ledgerwatch/erigon-lib/common/hexutility"
	types2 "github.com/ledgerwatch/erigon-lib/types"
)

// SignedTransaction contains transaction payload and signature as defined in EIP-6493a
// TODO: maybe this belongs in the cl package?
// TODO: json tags are not final

type SignedTransaction struct {
	Payload   TransactionPayload   `json:"payload"`
	Signature TransactionSignature `json:"signature"`
}

type TransactionPayload struct {
	Type                hexutil.Uint       `json:"type"`
	ChainID             *uint256.Int       `json:"chain_id,omitempty"`
	Nonce               hexutil.Uint64     `json:"nonce"`
	GasPrice            uint256.Int        `json:"max_fee_per_gas,omitempty"`
	Gas                 hexutil.Uint64     `json:"gas"`
	To                  *common.Address    `json:"to,omitempty"`
	Value               uint256.Int        `json:"value"`
	Input               hexutility.Bytes   `json:"input"`
	Accesses            *types2.AccessList `json:"access_list,omitempty"`
	Tip                 *uint256.Int       `json:"max_priority_fee_per_gas,omitempty"`
	MaxFeePerBlobGas    *uint256.Int       `json:"max_fee_per_blob_gas,omitempty"`
	BlobVersionedHashes *[]common.Hash     `json:"blob_versioned_hashes,omitempty"`
}

type TransactionSignature struct {
	From      common.Address   `json:"from"`
	Signature hexutility.Bytes `json:"ecdsa_signature"` // TODO: this needs to be of particular size (see EIP)
}

type TxVariant uint

const (
	LegacyTxnType TxVariant = iota
	DynamicFeeTxnType
	BlobTxnType
	AccessListTxnType
	SSZTxnType
	BasicTxnType
	ReplayableTxnType
)

func (t *SignedTransaction) GetVariant() TxVariant {
	switch int(t.Payload.Type) {
	case SSZTxType:
		if t.Payload.BlobVersionedHashes != nil {
			return BlobTxnType
		}
		return BasicTxnType

	case BlobTxType:
		return BlobTxnType

	case DynamicFeeTxType:
		return DynamicFeeTxnType

	case AccessListTxType:
		return AccessListTxnType

	default:
		if t.Payload.ChainID != nil {
			return LegacyTxnType
		}
		return ReplayableTxnType
	}
}

func UnmarshalTransctionFromJson(signer Signer, data []byte, blobTxnsAreWrappedWithBlobs bool) (Transaction, error) {
	tx := &SignedTransaction{}
	err := json.Unmarshal(data, tx)
	if err != nil {
		return nil, err
	}

	legacyTx := LegacyTx{
		CommonTx: CommonTx{
			TransactionMisc: TransactionMisc{},
			Nonce:           tx.Payload.Nonce.Uint64(),
			Gas:             tx.Payload.Gas.Uint64(),
			To:              tx.Payload.To,
			Value:           &tx.Payload.Value,
			Data:            tx.Payload.Input,
		},
		GasPrice: &tx.Payload.GasPrice,
	}

	var txi Transaction
	variant := tx.GetVariant()
	switch variant {
	case SSZTxType:
		blobTx := &BlobTx{
			DynamicFeeTransaction: DynamicFeeTransaction{
				CommonTx:   legacyTx.CommonTx,
				ChainID:    tx.Payload.ChainID,
				Tip:        tx.Payload.Tip,
				FeeCap:     &tx.Payload.GasPrice,
				AccessList: *tx.Payload.Accesses,
			},
			BlobVersionedHashes: *tx.Payload.BlobVersionedHashes,
		}
		txi = &SSZTransaction{
			BlobTx: *blobTx,
		}

	case BasicTxnType:
		blobTx := &BlobTx{
			DynamicFeeTransaction: DynamicFeeTransaction{
				CommonTx:   legacyTx.CommonTx,
				ChainID:    tx.Payload.ChainID,
				Tip:        tx.Payload.Tip,
				FeeCap:     &tx.Payload.GasPrice,
				AccessList: *tx.Payload.Accesses,
			},
		}
		txi = &SSZTransaction{
			BlobTx: *blobTx,
		}

	case BlobTxnType:
		txi = &BlobTx{
			DynamicFeeTransaction: DynamicFeeTransaction{
				CommonTx:   legacyTx.CommonTx,
				ChainID:    tx.Payload.ChainID,
				Tip:        tx.Payload.Tip,
				FeeCap:     &tx.Payload.GasPrice,
				AccessList: *tx.Payload.Accesses,
			},
			BlobVersionedHashes: *tx.Payload.BlobVersionedHashes,
		}

	case DynamicFeeTxnType:
		txi = &DynamicFeeTransaction{
			CommonTx:   legacyTx.CommonTx,
			ChainID:    tx.Payload.ChainID,
			Tip:        tx.Payload.Tip,
			FeeCap:     &tx.Payload.GasPrice,
			AccessList: *tx.Payload.Accesses,
		}

	case AccessListTxnType:
		txi = &AccessListTx{
			LegacyTx:   legacyTx,
			ChainID:    tx.Payload.ChainID,
			AccessList: *tx.Payload.Accesses,
		}

	case LegacyTxnType, ReplayableTxnType:
		txi = &legacyTx

	default:
		log.Fatalf("unknown transaction type: %d", variant)
		return nil, nil
	}

	txi, err = txi.WithSignature(signer, tx.Signature.Signature[:])
	if err != nil {
		return nil, err
	}

	// validate transaction

	v, r, s := txi.RawSignatureValues()
	maybeProtected := txi.Type() == LegacyTxType
	if err = sanityCheckSignature(v, r, s, maybeProtected); err != nil {
		return nil, err
	}

	// sender check

	txiSender, err := txi.Sender(signer)
	if err != nil {
		return nil, fmt.Errorf("failed at sender recovery")
	}

	if tx.Signature.From != txiSender {
		return nil, fmt.Errorf("sender mismatch: expected %v, got %v", tx.Signature.From, txiSender)
	}

	txi.SetSender(tx.Signature.From)

	return txi, nil
}

func DecodeTransactionsJson(signer Signer, txs [][]byte) ([]Transaction, error) {
	result := make([]Transaction, len(txs))
	var err error
	for i := range txs {
		result[i], err = UnmarshalTransctionFromJson(signer, txs[i], false /* blobTxnsAreWrappedWithBlobs*/)
		if err != nil {
			return nil, err
		}
	}

	return result, nil
}
