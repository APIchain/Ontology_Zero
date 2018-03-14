/*
 * Copyright (C) 2018 Onchain <onchain@onchain.com>
 *
 * This file is part of The ontology_Zero.
 *
 * The ontology_Zero is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Lesser General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * The ontology_Zero is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Lesser General Public License for more details.
 *
 * You should have received a copy of the GNU Lesser General Public License
 * along with The ontology_Zero.  If not, see <http://www.gnu.org/licenses/>.
 */

package solo

import (
	"fmt"
	cl "github.com/Ontology/account"
	. "github.com/Ontology/common"
	"github.com/Ontology/common/config"
	"github.com/Ontology/common/log"
	"github.com/Ontology/core/contract"
	"github.com/Ontology/core/contract/program"
	"github.com/Ontology/core/ledger"
	sig "github.com/Ontology/core/signature"
	tx "github.com/Ontology/core/transaction"
	"github.com/Ontology/core/transaction/payload"
	"github.com/Ontology/core/transaction/utxo"
	"github.com/Ontology/crypto"
	"github.com/Ontology/net"
	"time"
)

/*
*Simple consensus for solo node in test environment.
 */
var GenBlockTime = (config.DEFAULTGENBLOCKTIME * time.Second)

const ContextVersion uint32 = 0

type SoloService struct {
	Client   cl.Client
	localNet net.Neter
	existCh  chan interface{}
}

func NewSoloService(client cl.Client, localNet net.Neter) *SoloService {
	return &SoloService{
		Client:   client,
		localNet: localNet,
	}
}

func (this *SoloService) Start() error {
	timer := time.NewTicker(GenBlockTime)
	go func() {
		defer timer.Stop()
		for {
			select {
			case <-timer.C:
				this.genBlock()
			case <-this.existCh:
				return
			}
		}
	}()
	return nil
}

func (this *SoloService) genBlock() {
	block := this.makeBlock()
	if block == nil {
		return
	}
	err := ledger.DefaultLedger.Blockchain.AddBlock(block)
	if err != nil {
		log.Errorf("Blockchain.AddBlock error:%s", err)
		return
	}
	err = this.localNet.CleanSubmittedTransactions(block)
	if err != nil {
		log.Errorf("CleanSubmittedTransactions error:%s", err)
		return
	}
}

func (this *SoloService) makeBlock() *ledger.Block {
	log.Debug()
	ac, _ := this.Client.GetDefaultAccount()
	owner := ac.PublicKey
	nextBookKeeper, err := ledger.GetBookKeeperAddress([]*crypto.PubKey{owner})
	if err != nil {
		log.Error("SoloService GetBookKeeperAddress error:%s", err)
		return nil
	}
	transactionsPool := this.localNet.GetTxnPool(true)
	nonce := GetNonce()
	txBookkeeping := this.createBookkeepingTransaction(nonce)

	transactions := make([]*tx.Transaction, 0, len(transactionsPool)+1)
	transactions = append(transactions, txBookkeeping)
	for _, transaction := range transactionsPool {
		transactions = append(transactions, transaction)
	}

	prevHash := ledger.DefaultLedger.Blockchain.CurrentBlockHash()
	height := ledger.DefaultLedger.Blockchain.BlockHeight + 1

	txHash := []Uint256{}
	for _, t := range transactions {
		txHash = append(txHash, t.Hash())
	}
	txRoot, err := crypto.ComputeRoot(txHash)
	if err != nil {
		log.Errorf("ComputeRoot error:%s", err)
		return nil
	}

	blockRoot := ledger.DefaultLedger.Store.GetBlockRootWithNewTxRoot(txRoot)
	header := &ledger.Header{
		Version:          ContextVersion,
		PrevBlockHash:    prevHash,
		TransactionsRoot: txRoot,
		BlockRoot:        blockRoot,
		Timestamp:        uint32(time.Now().Unix()),
		Height:           height,
		ConsensusData:    nonce,
		NextBookKeeper:   nextBookKeeper,
	}
	block := &ledger.Block{
		Header:       header,
		Transactions: transactions,
	}

	programs, err := this.getBlockPrograms(block, owner)
	if err != nil {
		log.Errorf("getBlockPrograms error:%s", err)
		return nil
	}
	if programs == nil {
		log.Errorf("getBlockPrograms programs is nil")
		return nil
	}

	block.SetPrograms(programs)
	return block
}

func (this *SoloService) getBlockPrograms(block *ledger.Block, owner *crypto.PubKey) ([]*program.Program, error) {
	ctx := contract.NewContractContext(block)
	account, _ := this.Client.GetAccount(owner)
	sigData, err := sig.SignBySigner(block, account)
	if err != nil {
		return nil, fmt.Errorf("SignBySigner error:%s", err)
	}

	sc, err := contract.CreateSignatureContract(owner)
	if err != nil {
		return nil, fmt.Errorf("CreateSignatureContract error:%s", err)
	}

	err = ctx.AddContract(sc, owner, sigData)
	if err != nil {
		return nil, fmt.Errorf("AddContract error:%s", err)
	}
	return ctx.GetPrograms(), nil
}

func (this *SoloService) createBookkeepingTransaction(nonce uint64) *tx.Transaction {
	log.Debug()
	//TODO: sysfee
	bookKeepingPayload := &payload.BookKeeping{
		Nonce: uint64(time.Now().UnixNano()),
	}

	return &tx.Transaction{
		TxType:         tx.BookKeeping,
		PayloadVersion: payload.BookKeepingPayloadVersion,
		Payload:        bookKeepingPayload,
		Attributes:     []*tx.TxAttribute{},
		UTXOInputs:     []*utxo.UTXOTxInput{},
		BalanceInputs:  []*tx.BalanceTxInput{},
		Outputs:        []*utxo.TxOutput{},
		Programs:       []*program.Program{},
	}
}

func (this *SoloService) Halt() error {
	close(this.existCh)
	return nil
}
