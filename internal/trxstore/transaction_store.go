package trxstore

import (
	"bytes"
	"errors"
	"fmt"
	"sync"

	"github.com/koinos/koinos-proto-golang/koinos"
	"github.com/koinos/koinos-proto-golang/koinos/protocol"
	"github.com/koinos/koinos-proto-golang/koinos/transaction_store"

	"google.golang.org/protobuf/proto"
)

var (
	// ErrSerialization occurs when a type cannot be serialized correctly
	ErrSerialization = errors.New("error serializing type")

	// ErrDeserialization occurs when a type cannot be deserialized correctly
	ErrDeserialization = errors.New("error deserializing type")

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
func (handler *TransactionStore) AddIncludedTransaction(tx *protocol.Transaction, topology *koinos.BlockTopology) error {
	handler.rwmutex.Lock()
	defer handler.rwmutex.Unlock()

	itemBytes, err := handler.backend.Get(tx.Id)
	if err != nil {
		return fmt.Errorf("%w, %v", ErrBackend, err)
	}

	if len(itemBytes) == 0 {
		item := transaction_store.TransactionItem{
			Transaction:      tx,
			ContainingBlocks: [][]byte{topology.Id},
		}

		itemBytes, err = proto.Marshal(&item)
		if err != nil {
			return fmt.Errorf("%w, %v", ErrSerialization, err)
		}

		err = handler.backend.Put(tx.Id, itemBytes)
		if err != nil {
			return fmt.Errorf("%w, %v", ErrBackend, err)
		}
	} else {
		item := &transaction_store.TransactionItem{}
		if err := proto.Unmarshal(itemBytes, item); err != nil {
			return fmt.Errorf("%w, %v", ErrDeserialization, err)
		}

		for _, blockID := range item.ContainingBlocks {
			if bytes.Equal(blockID, topology.Id) {
				return nil
			}
		}

		item.ContainingBlocks = append(item.ContainingBlocks, topology.Id)
		itemBytes, err = proto.Marshal(item)
		if err != nil {
			return fmt.Errorf("%w, %v", ErrSerialization, err)
		}

		err = handler.backend.Put(tx.Id, itemBytes)
		if err != nil {
			return fmt.Errorf("%w, %v", ErrBackend, err)
		}
	}

	return nil
}

// GetTransactionsByID returns transactions by transaction ID
func (handler *TransactionStore) GetTransactionsByID(trxIDs [][]byte) ([]*transaction_store.TransactionItem, error) {
	trxs := make([]*transaction_store.TransactionItem, 0)

	handler.rwmutex.RLock()
	defer handler.rwmutex.RUnlock()

	for _, tid := range trxIDs {
		if tid == nil {
			return nil, errors.New("transaction id was nil")
		}
		itemBytes, err := handler.backend.Get(tid)
		if err != nil {
			return nil, fmt.Errorf("%w, %v", ErrBackend, err)
		}
		if len(itemBytes) != 0 {
			item := &transaction_store.TransactionItem{}
			if err := proto.Unmarshal(itemBytes, item); err != nil {
				return nil, fmt.Errorf("%w, %v", ErrDeserialization, err)
			}

			trxs = append(trxs, item)
		}
	}

	return trxs, nil
}
