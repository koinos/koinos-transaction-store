package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/dgraph-io/badger/v3"
	log "github.com/koinos/koinos-log-golang/v2"
	koinosmq "github.com/koinos/koinos-mq-golang"
	"github.com/koinos/koinos-proto-golang/v2/koinos"
	"github.com/koinos/koinos-proto-golang/v2/koinos/broadcast"
	"github.com/koinos/koinos-proto-golang/v2/koinos/rpc"
	"github.com/koinos/koinos-proto-golang/v2/koinos/rpc/transaction_store"
	"github.com/koinos/koinos-transaction-store/internal/trxstore"
	util "github.com/koinos/koinos-util-golang/v2"
	flag "github.com/spf13/pflag"
)

const (
	basedirOption     = "basedir"
	amqpOption        = "amqp"
	instanceIDOption  = "instance-id"
	logLevelOption    = "log-level"
	logDirOption      = "log-dir"
	logColorOption    = "log-color"
	logDatetimeOption = "log-datetime"
	resetOption       = "reset"
	jobsOption        = "jobs"
	versionOption     = "version"
)

const (
	basedirDefault     = ".koinos"
	amqpDefault        = "amqp://guest:guest@localhost:5672/"
	instanceIDDefault  = ""
	logLevelDefault    = "info"
	logColorDefault    = true
	logDatetimeDefault = true
	resetDefault       = false
)

const (
	trxStoreRPC = "transaction_store"
	blockAccept = "koinos.block.accept"
	appName     = "transaction_store"
)

// Version display values
const (
	DisplayAppName = "Koinos Transaction Store"
	Version        = "v1.1.0"
)

// Gets filled in by the linker
var Commit string

func main() {
	jobsDefault := runtime.NumCPU()

	var baseDir string

	baseDirPtr := flag.StringP(basedirOption, "d", basedirDefault, "Koinos base directory")
	amqp := flag.StringP(amqpOption, "a", "", "AMQP server URL")
	reset := flag.BoolP("reset", "r", false, "Reset the database")
	instanceID := flag.StringP(instanceIDOption, "i", instanceIDDefault, "The instance ID to identify this service")
	logLevel := flag.StringP(logLevelOption, "l", logLevelDefault, "The log filtering level (debug, info, warning, error)")
	logDir := flag.String(logDirOption, "", "The logging directory")
	logColor := flag.Bool(logColorOption, logColorDefault, "Log color toggle")
	logDatetime := flag.Bool(logDatetimeOption, logDatetimeDefault, "Log datetime on console toggle")
	jobs := flag.IntP(jobsOption, "j", jobsDefault, "Number of RPC jobs to run")
	version := flag.BoolP(versionOption, "v", false, "Print version and exit")

	flag.Parse()

	if *version {
		fmt.Println(makeVersionString())
		os.Exit(0)
	}

	baseDir, err := util.InitBaseDir(*baseDirPtr)
	if err != nil {
		fmt.Printf("Could not initialize base directory '%v'\n", baseDir)
		os.Exit(1)
	}

	yamlConfig := util.InitYamlConfig(baseDir)

	*amqp = util.GetStringOption(amqpOption, amqpDefault, *amqp, yamlConfig.TransactionStore, yamlConfig.Global)
	*logLevel = util.GetStringOption(logLevelOption, logLevelDefault, *logLevel, yamlConfig.TransactionStore, yamlConfig.Global)
	*logDir = util.GetStringOption(logDirOption, *logDir, *logDir, yamlConfig.TransactionStore, yamlConfig.Global)
	*logColor = util.GetBoolOption(logColorOption, logColorDefault, *logColor, yamlConfig.TransactionStore, yamlConfig.Global)
	*logDatetime = util.GetBoolOption(logDatetimeOption, logDatetimeDefault, *logDatetime, yamlConfig.TransactionStore, yamlConfig.Global)
	*instanceID = util.GetStringOption(instanceIDOption, util.GenerateBase58ID(5), *instanceID, yamlConfig.TransactionStore, yamlConfig.Global)
	*reset = util.GetBoolOption(resetOption, resetDefault, *reset, yamlConfig.TransactionStore, yamlConfig.Global)
	*jobs = util.GetIntOption(jobsOption, jobsDefault, *jobs, yamlConfig.TransactionStore, yamlConfig.Global)

	if len(*logDir) > 0 && !path.IsAbs(*logDir) {
		*logDir = path.Join(util.GetAppDir(baseDir, appName), *logDir)
	}

	err = log.InitLogger(appName, *instanceID, *logLevel, *logDir, *logColor, *logDatetime)
	if err != nil {
		panic(fmt.Sprintf("Invalid log-level: %s. Please choose one of: debug, info, warning, error", *logLevel))
	}

	log.Info(makeVersionString())

	// Costruct the db directory and ensure it exists
	dbDir := path.Join(util.GetAppDir((baseDir), appName), "db")
	err = util.EnsureDir(dbDir)
	if err != nil {
		log.Errorf("Could not create database folder %v", dbDir)
		os.Exit(1)
	}

	log.Infof("Opening database at %s", dbDir)

	var opts = badger.DefaultOptions(dbDir)
	opts.Logger = trxstore.KoinosBadgerLogger{}
	var backend = trxstore.NewBadgerBackend(opts)

	// Reset backend if requested
	if *reset {
		log.Info("Resetting database")
		err := backend.Reset()
		if err != nil {
			panic(fmt.Sprintf("Error resetting database: %s\n", err.Error()))
		}
	}

	requestHandler := koinosmq.NewRequestHandler(*amqp, uint(*jobs), koinosmq.ExponentialBackoff)
	trxStore := trxstore.NewTransactionStore(backend)

	requestHandler.SetRPCHandler(trxStoreRPC, func(rpcType string, data []byte) ([]byte, error) {
		request := &transaction_store.TransactionStoreRequest{}
		response := &transaction_store.TransactionStoreResponse{}

		err := proto.Unmarshal(data, request)

		if err != nil {
			log.Warnf("Received malformed request: %v", data)
		} else {
			log.Debugf("Received RPC request: %s", request.String())
			switch v := request.Request.(type) {
			case *transaction_store.TransactionStoreRequest_GetTransactionsById:
				if v.GetTransactionsById.TransactionIds == nil {
					err = errors.New("expected field transaction_ids was nil")
					break
				}

				if result, err := trxStore.GetTransactionsByID(v.GetTransactionsById.TransactionIds); err == nil {
					r := &transaction_store.GetTransactionsByIdResponse{Transactions: result}
					response.Response = &transaction_store.TransactionStoreResponse_GetTransactionsById{GetTransactionsById: r}
				}
			default:
				err = errors.New("unknown request")
			}
		}

		if err != nil {
			e := &rpc.ErrorStatus{Message: string(err.Error())}
			response.Response = &transaction_store.TransactionStoreResponse_Error{Error: e}
		}

		return proto.Marshal(response)
	})

	var recentTransactions uint32

	requestHandler.SetBroadcastHandler(blockAccept, func(topic string, data []byte) {
		submission := &broadcast.BlockAccepted{}

		if err := proto.Unmarshal(data, submission); err != nil {
			log.Warnf("Unable to parse koinos.block.accept broadcast: %v", data)
			return
		}

		if submission.GetLive() {
			log.Debugf("Received broadcasted block - Height: %d, ID: 0x%s", submission.Block.Header.Height, hex.EncodeToString(submission.Block.Id))
		} else if submission.GetBlock().GetHeader().GetHeight()%1000 == 0 {
			log.Infof("Sync block progress - Height: %d, ID: 0x%s", submission.Block.Header.Height, hex.EncodeToString(submission.Block.Id))
		}

		topology := koinos.BlockTopology{
			Id:       submission.Block.Id,
			Height:   submission.Block.Header.Height,
			Previous: submission.Block.Header.Previous,
		}

		for _, trx := range submission.Block.Transactions {
			if err := trxStore.AddIncludedTransaction(trx, &topology); err != nil {
				log.Warnf("could not add included transaction: %s", err)
			} else {
				atomic.AddUint32(&recentTransactions, 1)
			}

		}
	})

	ctx, ctxCancel := context.WithCancel(context.Background())
	requestHandler.Start(ctx)

	go func() {
		for {
			select {
			case <-time.After(60 * time.Second):
				NumTransactions := atomic.SwapUint32(&recentTransactions, 0)

				if NumTransactions > 0 {
					log.Infof("Recently added %v transaction(s)", NumTransactions)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Wait for a SIGINT or SIGTERM signal
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	log.Info("Shutting down node...")
	ctxCancel()
	backend.Close()
}

func makeVersionString() string {
	commitString := ""
	if len(Commit) >= 8 {
		commitString = fmt.Sprintf("(%s)", Commit[0:8])
	}

	return fmt.Sprintf("%s %s %s", DisplayAppName, Version, commitString)
}
