package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path"
	"syscall"

	"github.com/dgraph-io/badger"
	log "github.com/koinos/koinos-log-golang"
	koinosmq "github.com/koinos/koinos-mq-golang"
	"github.com/koinos/koinos-transaction-store/internal/trxstore"
	types "github.com/koinos/koinos-types-golang"
	util "github.com/koinos/koinos-util-golang"
	flag "github.com/spf13/pflag"
)

const (
	basedirOption    = "basedir"
	amqpOption       = "amqp"
	instanceIDOption = "instance-id"
	logLevelOption   = "log-level"
)

const (
	basedirDefault    = ".koinos"
	amqpDefault       = "amqp://guest:guest@localhost:5672/"
	instanceIDDefault = ""
	logLevelDefault   = "info"
)

const (
	trxStoreRPC = "transaction_store"
	blockAccept = "koinos.block.accept"
	appName     = "transaction_store"
	logDir      = "logs"
)

func main() {
	var baseDir = flag.StringP(basedirOption, "d", basedirDefault, "the base directory")
	var amqp = flag.StringP(amqpOption, "a", "", "AMQP server URL")
	var reset = flag.BoolP("reset", "r", false, "reset the database")
	instanceID := flag.StringP(instanceIDOption, "i", instanceIDDefault, "The instance ID to identify this service")
	logLevel := flag.StringP(logLevelOption, "v", logLevelDefault, "The log filtering level (debug, info, warn, error)")

	flag.Parse()

	*baseDir = util.InitBaseDir(*baseDir)

	yamlConfig := util.InitYamlConfig(*baseDir)

	*amqp = util.GetStringOption(amqpOption, amqpDefault, *amqp, yamlConfig.TransactionStore, yamlConfig.Global)
	*logLevel = util.GetStringOption(logLevelOption, logLevelDefault, *logLevel, yamlConfig.TransactionStore, yamlConfig.Global)
	*instanceID = util.GetStringOption(instanceIDOption, util.GenerateBase58ID(5), *instanceID, yamlConfig.TransactionStore, yamlConfig.Global)

	appID := fmt.Sprintf("%s.%s", appName, *instanceID)

	// Initialize logger
	logFilename := path.Join(util.GetAppDir(*baseDir, appName), logDir, "block_store.log")
	err := log.InitLogger(*logLevel, false, logFilename, appID)
	if err != nil {
		panic(fmt.Sprintf("Invalid log-level: %s. Please choose one of: debug, info, warn, error", *logLevel))
	}

	// Costruct the db directory and ensure it exists
	dbDir := path.Join(util.GetAppDir((*baseDir), appName), "db")
	util.EnsureDir(dbDir)
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

	defer backend.Close()

	requestHandler := koinosmq.NewRequestHandler(*amqp)
	trxStore := trxstore.NewTransactionStore(backend)

	requestHandler.SetRPCHandler(trxStoreRPC, func(rpcType string, data []byte) ([]byte, error) {
		req := types.NewTransactionStoreRequest()
		resp := types.NewTransactionStoreResponse()

		err := json.Unmarshal(data, req)
		if err != nil {
			log.Warnf("Received malformed request: %s", string(data))
			resp.Value = &types.BlockStoreErrorResponse{ErrorText: types.String(err.Error())}
		} else {
			log.Debugf("Received RPC request: %s", string(data))
			var err error
			switch v := req.Value.(type) {
			case *types.TransactionStoreReservedRequest:
				err = errors.New("Rerserved request is not supported")
				break
			case *types.GetTransactionsByIDRequest:
				result, err := trxStore.GetTransactionsByID(v.TransactionIds)
				if err == nil {
					resp.Value = &types.GetTransactionsByIDResponse{Transactions: result}
				}
			default:
				err = errors.New("Unknown request")
			}

			if err != nil {
				resp.Value = types.TransactionStoreErrorResponse{ErrorText: types.String(err.Error())}
			}
		}

		var outputBytes []byte
		outputBytes, err = json.Marshal(&resp)

		return outputBytes, err
	})

	requestHandler.SetBroadcastHandler(blockAccept, func(topic string, data []byte) {
		sub := types.NewBlockAccepted()
		err := json.Unmarshal(data, sub)

		if err != nil {
			log.Warnf("Unable to parse koinos.block.accept broadcast: %s", string(data))
			return
		}

		log.Infof("Received broadcasted block - %s", util.BlockString(&sub.Block))

		topology := types.BlockTopology{
			ID:       sub.Block.ID,
			Height:   sub.Block.Header.Height,
			Previous: sub.Block.Header.Previous,
		}

		for _, trx := range sub.Block.Transactions {
			trxStore.AddIncludedTransaction(&trx, &topology)
		}
	})

	requestHandler.Start()

	// Wait for a SIGINT or SIGTERM signal
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	log.Info("Shutting down node...")
}
