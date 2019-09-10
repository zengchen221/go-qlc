package api

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/qlcchain/go-qlc/common"
	"github.com/qlcchain/go-qlc/common/event"
	"github.com/qlcchain/go-qlc/config"
	"github.com/qlcchain/go-qlc/ledger"
	"github.com/qlcchain/go-qlc/ledger/relation"
	"github.com/qlcchain/go-qlc/mock"

	"github.com/qlcchain/go-qlc/common/types"
)

func setupTestCaseLedger(t *testing.T) (func(t *testing.T), *ledger.Ledger, *LedgerApi) {
	t.Parallel()

	dir := filepath.Join(config.QlcTestDataDir(), "rewards", uuid.New().String())
	_ = os.RemoveAll(dir)
	l := ledger.NewLedger(dir)
	cm := config.NewCfgManager(dir)
	_, _ = cm.Load()
	rl, err := relation.NewRelation(cm.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	eb := event.GetEventBus(dir)
	ledgerApi := NewLedgerApi(l, rl, eb)

	return func(t *testing.T) {
		//err := l.Store.Erase()
		err := l.Close()
		if err != nil {
			t.Fatal(err)
		}
		err = rl.Close()
		if err != nil {
			t.Fatal(err)
		}
		//CloseLedger()
		err = os.RemoveAll(dir)
		if err != nil {
			t.Fatal(err)
		}
	}, l, ledgerApi
}

func TestLedger_GetBlockCacheLock(t *testing.T) {
	teardownTestCase, _, ledgerApi := setupTestCaseLedger(t)
	defer teardownTestCase(t)

	chainToken := common.ChainToken()
	gasToken := common.GasToken()
	addr, _ := types.HexToAddress("qlc_361j3uiqdkjrzirttrpu9pn7eeussymty4rz4gifs9ijdx1p46xnpu3je7sy")
	_ = ledgerApi.getProcessLock(addr, chainToken)
	if ledgerApi.processLock.Len() != 1 {
		t.Fatal("lock len error for addr")
	}
	_ = ledgerApi.getProcessLock(addr, gasToken)
	if ledgerApi.processLock.Len() != 2 {
		t.Fatal("lock error for different token")
	}

	for i := 0; i < 998; i++ {
		a := mock.Address()
		ledgerApi.getProcessLock(a, chainToken)
	}
	if ledgerApi.processLock.Len() != 1000 {
		t.Fatal("lock len error for 1000 addresses")
	}
	sb := mock.StateBlockWithAddress(addr)
	_, _ = ledgerApi.Process(sb)
	addr2, _ := types.HexToAddress("qlc_1gnggt8b6cwro3b4z9gootipykqd6x5gucfd7exsi4xqkryiijciegfhon4u")
	_ = ledgerApi.getProcessLock(addr2, chainToken)
	fmt.Println(ledgerApi.processLock.Len())
	if ledgerApi.processLock.Len() != 1000 {
		t.Fatal("get error when delete idle lock")
	}
}
