// Copyright 2018 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package miner

import (
	"math/big"
	"testing"
	"time"

	"github.com/DxChainNetwork/godx/consensus"
	"github.com/DxChainNetwork/godx/consensus/dpos"
	"github.com/DxChainNetwork/godx/core"
	"github.com/DxChainNetwork/godx/core/types"
	"github.com/DxChainNetwork/godx/core/vm"
	"github.com/DxChainNetwork/godx/crypto"
	"github.com/DxChainNetwork/godx/ethdb"
	"github.com/DxChainNetwork/godx/event"
	"github.com/DxChainNetwork/godx/params"
)

var (
	// Test chain configurations
	testTxPoolConfig core.TxPoolConfig
	dposChainConfig  *params.ChainConfig

	// Test accounts
	testBankKey, _  = crypto.GenerateKey()
	testBankAddress = crypto.PubkeyToAddress(testBankKey.PublicKey)
	testBankFunds   = big.NewInt(1000000000000000000)

	testUserKey, _  = crypto.GenerateKey()
	testUserAddress = crypto.PubkeyToAddress(testUserKey.PublicKey)

	// Test transactions
	pendingTxs []*types.Transaction
	newTxs     []*types.Transaction
)

func init() {
	testTxPoolConfig = core.DefaultTxPoolConfig
	testTxPoolConfig.Journal = ""
	dposChainConfig = params.DposChainConfig

	tx1, _ := types.SignTx(types.NewTransaction(0, testUserAddress, big.NewInt(1000), params.TxGas, nil, nil), types.HomesteadSigner{}, testBankKey)
	pendingTxs = append(pendingTxs, tx1)
	tx2, _ := types.SignTx(types.NewTransaction(1, testUserAddress, big.NewInt(1000), params.TxGas, nil, nil), types.HomesteadSigner{}, testBankKey)
	newTxs = append(newTxs, tx2)
}

// testWorkerBackend implements worker.Backend interfaces and wraps all information needed during the testing.
type testWorkerBackend struct {
	db         ethdb.Database
	txPool     *core.TxPool
	chain      *core.BlockChain
	testTxFeed event.Feed
	uncleBlock *types.Block
}

func newTestWorkerBackend(t *testing.T, chainConfig *params.ChainConfig, engine consensus.Engine, n int) *testWorkerBackend {
	var (
		db    = ethdb.NewMemDatabase()
		gspec = core.Genesis{
			Config: chainConfig,
			Alloc:  core.GenesisAlloc{testBankAddress: {Balance: testBankFunds}},
		}
	)
	for _, validatorConfig := range chainConfig.Dpos.Validators {
		gspec.Alloc[validatorConfig.Address] = core.GenesisAccount{
			Balance: validatorConfig.Deposit.BigIntPtr(),
		}
	}
	switch engine.(type) {
	case *dpos.Dpos:
	default:
		t.Fatalf("unexpected consensus engine type: %T", engine)
	}
	genesis := gspec.MustCommit(db)

	chain, _ := core.NewBlockChain(db, nil, gspec.Config, engine, vm.Config{}, nil)
	txpool := core.NewTxPool(testTxPoolConfig, chainConfig, chain)

	// Generate a small n-block chain and an uncle block for it
	if n > 0 {
		blocks, _ := core.GenerateChain(chainConfig, genesis, engine, db, n, func(i int, gen *core.BlockGen) {
			gen.SetCoinbase(testBankAddress)
		})
		if _, err := chain.InsertChain(blocks); err != nil {
			t.Fatalf("failed to insert origin chain: %v", err)
		}
	}
	parent := genesis
	if n > 0 {
		parent = chain.GetBlockByHash(chain.CurrentBlock().ParentHash())
	}
	blocks, _ := core.GenerateChain(chainConfig, parent, engine, db, 1, func(i int, gen *core.BlockGen) {
		gen.SetCoinbase(testUserAddress)
	})

	return &testWorkerBackend{
		db:         db,
		chain:      chain,
		txPool:     txpool,
		uncleBlock: blocks[0],
	}
}

func (b *testWorkerBackend) BlockChain() *core.BlockChain { return b.chain }
func (b *testWorkerBackend) TxPool() *core.TxPool         { return b.txPool }
func (b *testWorkerBackend) PostChainEvents(events []interface{}) {
	b.chain.PostChainEvents(events, nil)
}

func newTestWorker(t *testing.T, chainConfig *params.ChainConfig, engine consensus.Engine, blocks int) (*worker, *testWorkerBackend) {
	backend := newTestWorkerBackend(t, chainConfig, engine, blocks)
	backend.txPool.AddLocals(pendingTxs)
	w := newWorker(chainConfig, engine, backend, new(event.TypeMux), time.Second, params.GenesisGasLimit, params.GenesisGasLimit, nil)
	w.setCoinbase(testBankAddress)
	return w, backend
}

func TestPendingStateAndBlockEthash(t *testing.T) {
	testPendingStateAndBlock(t, dposChainConfig, dpos.NewDposFaker())
}

func testPendingStateAndBlock(t *testing.T, chainConfig *params.ChainConfig, engine consensus.Engine) {
	defer engine.Close()

	w, b := newTestWorker(t, chainConfig, engine, 0)
	defer w.close()

	// Ensure snapshot has been updated.
	time.Sleep(100 * time.Millisecond)
	block, state := w.pending()
	if block.NumberU64() != 1 {
		t.Errorf("block number mismatch: have %d, want %d", block.NumberU64(), 1)
	}
	if balance := state.GetBalance(testUserAddress); balance.Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("account balance mismatch: have %d, want %d", balance, 1000)
	}
	b.txPool.AddLocals(newTxs)

	// Ensure the new tx events has been processed
	time.Sleep(100 * time.Millisecond)
	block, state = w.pending()
	if balance := state.GetBalance(testUserAddress); balance.Cmp(big.NewInt(2000)) != 0 {
		t.Errorf("account balance mismatch: have %d, want %d", balance, 2000)
	}
}

func TestCheckDposBalance(t *testing.T) {
	testCheckDposBalance(t, dposChainConfig, dpos.NewDposFaker())
}

func testCheckDposBalance(t *testing.T, chainConfig *params.ChainConfig, engine consensus.Engine) {
	defer engine.Close()

	w, _ := newTestWorker(t, chainConfig, engine, 0)
	defer w.close()

	var (
		taskCh    = make(chan struct{}, 2)
		taskIndex int
	)

	checkEqual := func(t *testing.T, task *task, index int) {
		receiptLen, balance := 0, big.NewInt(0)
		if index == 0 {
			receiptLen, balance = 1, big.NewInt(1000)
		}
		if len(task.receipts) != receiptLen {
			t.Errorf("receipt number mismatch: have %d, want %d", len(task.receipts), receiptLen)
		}
		if task.state.GetBalance(testUserAddress).Cmp(balance) != 0 {
			t.Errorf("account balance mismatch: have %d, want %d", task.state.GetBalance(testUserAddress), balance)
		}
	}

	w.newTaskHook = func(task *task) {
		if task.block.NumberU64() == 1 {
			checkEqual(t, task, taskIndex)
			taskIndex += 1
			taskCh <- struct{}{}
		}
	}
	w.fullTaskHook = func() {
		time.Sleep(100 * time.Millisecond)
	}

	// Ensure worker has finished initialization
	for {
		b := w.pendingBlock()
		if b != nil && b.NumberU64() == 1 {
			break
		}
	}

	w.start()

	select {
	case <-taskCh:
	case <-time.NewTimer(2 * time.Second).C:
		t.Error("new task timeout")
	}

}

func TestStreamUncleBlock(t *testing.T) {
	dposEngine := dpos.NewDposFaker()
	defer dposEngine.Close()

	w, b := newTestWorker(t, dposChainConfig, dposEngine, 1)
	defer w.close()

	var taskCh = make(chan struct{})

	taskIndex := 0
	w.newTaskHook = func(task *task) {
		if task.block.NumberU64() == 2 {
			if taskIndex == 2 {
				have := task.block.Header().UncleHash
				want := types.EmptyUncleHash
				if have != want {
					t.Errorf("uncle hash mismatch: have %s, want %s", have.Hex(), want.Hex())
				}
			}
			taskCh <- struct{}{}
			taskIndex += 1
		}
	}
	w.skipSealHook = func(task *task) bool {
		return true
	}
	w.fullTaskHook = func() {
		time.Sleep(100 * time.Millisecond)
	}

	// Ensure worker has finished initialization
	for {
		b := w.pendingBlock()
		if b != nil && b.NumberU64() == 2 {
			break
		}
	}
	w.start()

	// Ignore the first two works
	for i := 0; i < 2; i += 1 {
		select {
		case <-taskCh:
		case <-time.NewTimer(10 * time.Second).C:
			t.Error("new task timeout")
		}
	}

	b.PostChainEvents([]interface{}{core.ChainSideEvent{Block: b.uncleBlock}})

	select {
	case <-taskCh:
	case <-time.NewTimer(10 * time.Second).C:
		t.Error("new task timeout")
	}
}

func TestRegenerateMiningBlockEthash(t *testing.T) {
	testRegenerateMiningBlock(t, dposChainConfig, dpos.NewDposFaker())
}

func testRegenerateMiningBlock(t *testing.T, chainConfig *params.ChainConfig, engine consensus.Engine) {
	defer engine.Close()

	w, b := newTestWorker(t, chainConfig, engine, 0)
	defer w.close()

	var taskCh = make(chan struct{})

	taskIndex := 0
	w.newTaskHook = func(task *task) {
		if task.block.NumberU64() == 1 {
			if taskIndex == 2 {
				receiptLen, balance := 2, big.NewInt(2000)
				if len(task.receipts) != receiptLen {
					t.Errorf("receipt number mismatch: have %d, want %d", len(task.receipts), receiptLen)
				}
				if task.state.GetBalance(testUserAddress).Cmp(balance) != 0 {
					t.Errorf("account balance mismatch: have %d, want %d", task.state.GetBalance(testUserAddress), balance)
				}
			}
			taskCh <- struct{}{}
			taskIndex += 1
		}
	}
	w.skipSealHook = func(task *task) bool {
		return true
	}
	w.fullTaskHook = func() {
		time.Sleep(100 * time.Millisecond)
	}
	// Ensure worker has finished initialization
	for {
		b := w.pendingBlock()
		if b != nil && b.NumberU64() == 1 {
			break
		}
	}

	w.start()
	// Ignore the first two works
	for i := 0; i < 2; i += 1 {
		select {
		case <-taskCh:
		case <-time.NewTimer(10 * time.Second).C:
			t.Error("new task timeout")
		}
	}
	b.txPool.AddLocals(newTxs)
	time.Sleep(time.Second)

	select {
	case <-taskCh:
	case <-time.NewTimer(time.Second).C:
		t.Error("new task timeout")
	}
}
