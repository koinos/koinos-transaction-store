package trxstore

import (
	"bytes"
	"errors"
	"fmt"
	"sync"

	types "github.com/koinos/koinos-types-golang"
)

const (
	highestBlockKey = 0x01
)

var (
	// ErrDeserialization occurs when a type cannot be deserialized correctly
	ErrDeserialization = errors.New("error deserializing type")

	// ErrRecordLength occurs when there are hanging bytes after deserialization
	ErrRecordLength = errors.New("deserialization did not consume all bytes")

	// ErrBackend occurs when there is an error in the backend
	ErrBackend = errors.New("error in backend")
)

// TransactionStore contains a backend object and handles requests
type TransactionStore struct {
	backend TransactionStoreBackend
	rwmutex sync.RWMutex
}

// NewTransactionStore creates a new TransactionStore wrapping the provided backend
func NewTransactionStore(backend TransactionStoreBackend) *TransactionStore {
	return &TransactionStore{backend: backend}
}

// AddIncludedTransaction adds a transaction to with the associated block topology
func (handler *TransactionStore) AddIncludedTransaction(tx *types.Transaction, topology *types.BlockTopology) error {
	record := types.TransactionRecord{
		Transaction:      *tx,
		ContainingBlocks: []types.Multihash{topology.ID},
	}

	vbKey := tx.ID.Serialize(types.NewVariableBlob())

	handler.rwmutex.Lock()
	defer handler.rwmutex.Unlock()

	recordBytes, err := handler.backend.Get(*vbKey)
	if err != nil {
		return fmt.Errorf("%w, %v", ErrBackend, err)
	}

	if len(recordBytes) == 0 {
		vbValue := record.Serialize(types.NewVariableBlob())
		err := handler.backend.Put(*vbKey, *vbValue)
		if err != nil {
			return fmt.Errorf("%w, %v", ErrBackend, err)
		}
	} else {
		vbExistingValue := types.VariableBlob(recordBytes)
		consumed, record, err := types.DeserializeTransactionRecord(&vbExistingValue)
		if err != nil {
			return fmt.Errorf("%w, %v", ErrDeserialization, err)
		}
		if consumed != uint64(len(recordBytes)) {
			return fmt.Errorf("%w, %v", ErrRecordLength, err)
		}

		for _, blockID := range record.ContainingBlocks {
			if blockID.ID == topology.ID.ID && bytes.Equal(blockID.Digest, topology.ID.Digest) {
				return nil
			}
		}

		record.ContainingBlocks = append(record.ContainingBlocks, topology.ID)
		vbValue := record.Serialize(types.NewVariableBlob())
		err = handler.backend.Put(*vbKey, *vbValue)
		if err != nil {
			return fmt.Errorf("%w, %v", ErrBackend, err)
		}
	}

	return nil
}

// GetTransactionsByID returns transactions by transaction ID
func (handler *TransactionStore) GetTransactionsByID(trxIDs types.VectorMultihash) (types.VectorOptTransactionRecord, error) {
	trxs := types.VectorOptTransactionRecord(make([]types.OptTransactionRecord, 0))

	handler.rwmutex.RLock()
	defer handler.rwmutex.RUnlock()

	for _, tid := range trxIDs {
		vbKey := tid.Serialize(types.NewVariableBlob())
		optTrx := types.OptTransactionRecord{}

		recordBytes, err := handler.backend.Get(*vbKey)
		if err != nil {
			return nil, fmt.Errorf("%w, %v", ErrBackend, err)
		}
		if len(recordBytes) != 0 {
			vbValue := types.VariableBlob(recordBytes)
			consumed, record, err := types.DeserializeTransactionRecord(&vbValue)
			if err == nil && consumed == uint64(len(recordBytes)) {
				optTrx.Value = record
			}
		}

		trxs = append(trxs, optTrx)
	}

	return trxs, nil
}
