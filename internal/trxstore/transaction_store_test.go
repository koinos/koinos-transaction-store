package trxstore

import (
	"errors"
	"io/ioutil"
	"os"
	"testing"

	"github.com/dgraph-io/badger"

	types "github.com/koinos/koinos-types-golang"
)

const (
	MapBackendType    = 0
	BadgerBackendType = 1
)

var backendTypes = [...]int{MapBackendType, BadgerBackendType}

func NewBackend(backendType int) TransactionStoreBackend {
	var backend TransactionStoreBackend
	switch backendType {
	case MapBackendType:
		backend = NewMapBackend()
		break
	case BadgerBackendType:
		dirname, err := ioutil.TempDir(os.TempDir(), "trxstore-test-*")
		if err != nil {
			panic("unable to create temp directory")
		}
		opts := badger.DefaultOptions(dirname)
		backend = NewBadgerBackend(opts)
		break
	default:
		panic("unknown backend type")
	}
	return backend
}

func CloseBackend(b interface{}) {
	switch t := b.(type) {
	case *MapBackend:
		break
	case *BadgerBackend:
		t.Close()
		break
	default:
		panic("unknown backend type")
	}
}

type ErrorBackend struct {
}

func (backend *ErrorBackend) Reset() error {
	return nil
}

// Put returns an error
func (backend *ErrorBackend) Put(key []byte, value []byte) error {
	return errors.New("Error on put")
}

// Get gets an error
func (backend *ErrorBackend) Get(key []byte) ([]byte, error) {
	return nil, errors.New("Error on get")
}

type BadBackend struct {
}

func (backend *BadBackend) Reset() error {
	return nil
}

// Put returns an error
func (backend *BadBackend) Put(key []byte, value []byte) error {
	return nil
}

// Get gets an error
func (backend *BadBackend) Get(key []byte) ([]byte, error) {
	return []byte{0, 0, 255, 255, 255, 255, 255}, nil
}

type LongBackend struct {
}

func (backend *LongBackend) Reset() error {
	return nil
}

// Put returns an error
func (backend *LongBackend) Put(key []byte, value []byte) error {
	return nil
}

// Get gets an error
func (backend *LongBackend) Get(key []byte) ([]byte, error) {
	return []byte{2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, nil
}

func TestAddTransaction(t *testing.T) {
	// Add the transactions
	for bType := range backendTypes {
		b := NewBackend(bType)
		store := NewTransactionStore(b)

		// Test adding a transaction
		trx := types.NewTransaction()
		trx.ID.ID = 1
		topology := types.NewBlockTopology()
		topology.ID.ID = 1

		err := store.AddIncludedTransaction(trx, topology)
		if err != nil {
			t.Fatal("Error adding transaction: ", err)
		}

		trxs, err := store.GetTransactionsByID([]types.Multihash{{ID: 1, Digest: *types.NewVariableBlob()}})
		if err != nil {
			t.Fatal("Error getting transaction: ", err)
		}
		if len(trxs) != 1 {
			t.Fatal("Incorrect number of transactions returned")
		}
		if trxs[0].Value == nil {
			t.Fatal("Expected transaction not returned")
		}
		if trxs[0].Value.Transaction.ID.ID != 1 {
			t.Fatal("Wrong transaction returned")
		}
		if len(trxs[0].Value.ContainingBlocks) != 1 {
			t.Fatal("Cointaining block not included")
		}
		if trxs[0].Value.ContainingBlocks[0].ID != 1 {
			t.Fatal("Cointaining block not correct")
		}

		// Test adding an already existing transaction
		topology.ID.ID = 2
		err = store.AddIncludedTransaction(trx, topology)
		if err != nil {
			t.Fatal("Error adding transaction: ", err)
		}

		trxs, err = store.GetTransactionsByID([]types.Multihash{{ID: 1, Digest: *types.NewVariableBlob()}})
		if err != nil {
			t.Fatal("Error getting transaction: ", err)
		}
		if len(trxs) != 1 {
			t.Fatal("Incorrect number of transactions returned")
		}
		if trxs[0].Value == nil {
			t.Fatal("Expected transaction not returned")
		}
		if trxs[0].Value.Transaction.ID.ID != 1 {
			t.Fatal("Wrong transaction returned")
		}
		if len(trxs[0].Value.ContainingBlocks) != 2 {
			t.Fatal("Cointaining block not included")
		}
		if trxs[0].Value.ContainingBlocks[0].ID != 1 {
			t.Fatal("Cointaining blocks not correct")
		}
		if trxs[0].Value.ContainingBlocks[1].ID != 2 {
			t.Fatal("Cointaining blocks not correct")
		}

		// Test adding second transaction
		trx.ID.ID = 2
		err = store.AddIncludedTransaction(trx, topology)
		if err != nil {
			t.Fatal("Error adding transaction: ", err)
		}

		trxs, err = store.GetTransactionsByID([]types.Multihash{
			{ID: 1, Digest: *types.NewVariableBlob()},
			{ID: 2, Digest: *types.NewVariableBlob()},
			{ID: 3, Digest: *types.NewVariableBlob()}})

		if err != nil {
			t.Fatal("Error getting transaction: ", err)
		}
		if len(trxs) != 3 {
			t.Fatal("Incorrect number of transactions returned")
		}
		if trxs[0].Value == nil {
			t.Fatal("Expected transaction not returned")
		}
		if trxs[0].Value.Transaction.ID.ID != 1 {
			t.Fatal("Wrong transaction returned")
		}
		if trxs[1].Value == nil {
			t.Fatal("Expected transaction not returned")
		}
		if trxs[1].Value.Transaction.ID.ID != 2 {
			t.Fatal("Wrong transaction returned")
		}
		if trxs[2].Value != nil {
			t.Fatal("Unexpected transaction returned")
		}

		CloseBackend(b)
	}

	// Test error backend
	{
		store := NewTransactionStore(&ErrorBackend{})
		trx := types.NewTransaction()
		trx.ID.ID = 1
		topology := types.NewBlockTopology()
		topology.ID.ID = 1

		err := store.AddIncludedTransaction(trx, topology)
		if !errors.Is(err, ErrBackend) {
			t.Fatal("Got unexpected error adding transaction: ", err)
		}

		_, err = store.GetTransactionsByID([]types.Multihash{{ID: 1}})
		if !errors.Is(err, ErrBackend) {
			t.Fatal("Got unexpected error adding transaction: ", err)
		}
	}

	// Test bad record
	{
		store := NewTransactionStore(&BadBackend{})
		trx := types.NewTransaction()
		trx.ID.ID = 1
		topology := types.NewBlockTopology()
		topology.ID.ID = 1

		err := store.AddIncludedTransaction(trx, topology)
		if !errors.Is(err, ErrDeserialization) {
			t.Fatal("Got unexpected error adding transaction: ", err)
		}
	}

	// Test too long record
	{
		store := NewTransactionStore(&LongBackend{})
		trx := types.NewTransaction()
		trx.ID.ID = 1
		topology := types.NewBlockTopology()
		topology.ID.ID = 1

		err := store.AddIncludedTransaction(trx, topology)
		if !errors.Is(err, ErrRecordLength) {
			t.Fatal("Got unexpected error adding transaction: ", err)
		}
	}
}
