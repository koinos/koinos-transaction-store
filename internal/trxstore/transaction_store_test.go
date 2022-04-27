package trxstore

import (
	"bytes"
	"errors"
	"io/ioutil"
	"os"
	"testing"

	"github.com/dgraph-io/badger/v3"

	"github.com/koinos/koinos-proto-golang/koinos"
	"github.com/koinos/koinos-proto-golang/koinos/protocol"
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
		trx := &protocol.Transaction{}
		trx.Id = []byte{1}
		topology := &koinos.BlockTopology{}
		topology.Id = []byte{1}

		err := store.AddIncludedTransaction(trx, topology)
		if err != nil {
			t.Fatal("Error adding transaction: ", err)
		}

		trxs, err := store.GetTransactionsByID([][]byte{{1}})
		if err != nil {
			t.Fatal("Error getting transaction: ", err)
		}
		if len(trxs) != 1 {
			t.Fatal("Incorrect number of transactions returned")
		}
		if !bytes.Equal(trxs[0].Transaction.Id, []byte{1}) {
			t.Fatal("Wrong transaction returned")
		}
		if len(trxs[0].ContainingBlocks) != 1 {
			t.Fatal("Containing block not included")
		}
		if !bytes.Equal(trxs[0].ContainingBlocks[0], []byte{1}) {
			t.Fatal("Containing block not correct")
		}

		// Test adding an already existing transaction
		topology.Id = []byte{2}
		err = store.AddIncludedTransaction(trx, topology)
		if err != nil {
			t.Fatal("Error adding transaction: ", err)
		}

		trxs, err = store.GetTransactionsByID([][]byte{{1}})
		if err != nil {
			t.Fatal("Error getting transaction: ", err)
		}
		if len(trxs) != 1 {
			t.Fatal("Incorrect number of transactions returned")
		}
		if !bytes.Equal(trxs[0].Transaction.Id, []byte{1}) {
			t.Fatal("Wrong transaction returned")
		}
		if len(trxs[0].ContainingBlocks) != 2 {
			t.Fatal("Containing block not included")
		}
		if !bytes.Equal(trxs[0].ContainingBlocks[0], []byte{1}) {
			t.Fatal("Containing block not correct")
		}
		if !bytes.Equal(trxs[0].ContainingBlocks[1], []byte{2}) {
			t.Fatal("Containing block not correct")
		}

		// Test adding second transaction
		trx.Id = []byte{2}
		err = store.AddIncludedTransaction(trx, topology)
		if err != nil {
			t.Fatal("Error adding transaction: ", err)
		}

		trxs, err = store.GetTransactionsByID([][]byte{{1}, {2}, {3}})

		if err != nil {
			t.Fatal("Error getting transaction: ", err)
		}
		if len(trxs) != 2 {
			t.Fatal("Incorrect number of transactions returned")
		}
		if !bytes.Equal(trxs[0].Transaction.Id, []byte{1}) {
			t.Fatal("Wrong transaction returned")
		}
		if !bytes.Equal(trxs[1].Transaction.Id, []byte{2}) {
			t.Fatal("Wrong transaction returned")
		}

		CloseBackend(b)
	}

	// Test error backend
	{
		store := NewTransactionStore(&ErrorBackend{})
		trx := &protocol.Transaction{}
		trx.Id = []byte{1}
		topology := &koinos.BlockTopology{}
		topology.Id = []byte{1}

		err := store.AddIncludedTransaction(trx, topology)
		if !errors.Is(err, ErrBackend) {
			t.Fatal("Got unexpected error adding transaction: ", err)
		}

		_, err = store.GetTransactionsByID([][]byte{{1}})
		if !errors.Is(err, ErrBackend) {
			t.Fatal("Got unexpected error adding transaction: ", err)
		}
	}

	// Test bad record
	{
		store := NewTransactionStore(&BadBackend{})
		trx := &protocol.Transaction{}
		trx.Id = []byte{1}
		topology := &koinos.BlockTopology{}
		topology.Id = []byte{1}

		err := store.AddIncludedTransaction(trx, topology)
		if !errors.Is(err, ErrDeserialization) {
			t.Fatal("Got unexpected error adding transaction: ", err)
		}
	}
}
