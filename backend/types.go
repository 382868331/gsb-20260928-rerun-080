package main

import "time"

// Block is a manually submitted block. The server never contacts any node.
type Block struct {
	Hash       string        `json:"hash"`
	ParentHash string        `json:"parentHash"`
	Height     int64         `json:"height"`
	Weight     uint64        `json:"weight"`
	Timestamp  time.Time     `json:"timestamp"`
	Miner      string        `json:"miner"`
	ReceivedAt time.Time     `json:"-"`
	Txs        []Transaction `json:"transactions"`
}

type Transaction struct {
	Hash  string `json:"hash"`
	From  string `json:"from"`
	To    string `json:"to"`
	Value string `json:"value"`
}

const (
	StatusCanonical = "canonical"
	StatusForked    = "forked"
	StatusOrphan    = "orphan"

	TxConfirmed = "confirmed"
	TxForked    = "forked"
	TxOrphan    = "orphan"
)

type OccurrenceView struct {
	BlockHash string `json:"blockHash"`
	Height    int64  `json:"height"`
	Index     int    `json:"index"`
	Status    string `json:"status"`
}

type BlockView struct {
	Hash              string    `json:"hash"`
	ParentHash        string    `json:"parentHash"`
	Height            int64     `json:"height"`
	Weight            uint64    `json:"weight"`
	ChainWork         uint64    `json:"chainWork"`
	Timestamp         time.Time `json:"timestamp"`
	Miner             string    `json:"miner"`
	Status            string    `json:"status"`
	Confirmations     int64     `json:"confirmations"`
	TransactionHashes []string  `json:"transactionHashes"`
}

type TransactionView struct {
	Hash          string           `json:"hash"`
	From          string           `json:"from"`
	To            string           `json:"to"`
	Value         string           `json:"value"`
	Status        string           `json:"status"`
	Duplicate     bool             `json:"duplicate"`
	Confirmations int64            `json:"confirmations"`
	BlockHash     string           `json:"blockHash"`
	BlockHeight   int64            `json:"blockHeight"`
	Occurrences   []OccurrenceView `json:"occurrences"`
}

type Snapshot struct {
	Version         int64             `json:"version"`
	PreviousVersion int64             `json:"previousVersion"`
	CreatedAt       time.Time         `json:"createdAt"`
	Reason          string            `json:"reason"`
	TipHash         string            `json:"tipHash"`
	Height          int64             `json:"height"`
	Blocks          []BlockView       `json:"blocks"`
	Transactions    []TransactionView `json:"transactions"`

	blockIndex map[string]*BlockView
	txIndex    map[string]*TransactionView
}

type ImportResult struct {
	Imported        []string `json:"imported"`
	Skipped         []string `json:"skipped"`
	Invalid         []string `json:"invalid"`
	PreviousVersion int64    `json:"previousVersion"`
	CurrentVersion  int64    `json:"currentVersion"`
	TipHash         string   `json:"tipHash"`
	Reason          string   `json:"reason"`
	Changed         bool     `json:"changed"`
}

type BlockDetail struct {
	Version int64     `json:"version"`
	Block   Block     `json:"block"`
	View    BlockView `json:"view"`
}

type TransactionDetail struct {
	Version int64           `json:"version"`
	View    TransactionView `json:"view"`
}
