/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package pvtdatastorage

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hyperledger/fabric/common/flogging"
	"github.com/hyperledger/fabric/common/ledger/util/leveldbhelper"
	"github.com/hyperledger/fabric/core/ledger"
	"github.com/hyperledger/fabric/core/ledger/pvtdatapolicy"
	"github.com/golang/protobuf/proto"
	"github.com/hyperledger/fabric-protos-go/ledger/rwset"
	"github.com/willf/bitset"
)

var logger = flogging.MustGetLogger("pvtdatastorage")

// Provider provides handle to specific 'Store' that in turn manages
// private write sets for a ledger
type Provider struct {
	dbProvider *leveldbhelper.Provider
	pvtData    *PrivateDataConfig
}

// PrivateDataConfig encapsulates the configuration for private data storage on the ledger
type PrivateDataConfig struct {
	// PrivateDataConfig is used to configure a private data storage provider
	*ledger.PrivateDataConfig
	// StorePath is the filesystem path for private data storage.
	// It is internally computed by the ledger component,
	// so it is not in ledger.PrivateDataConfig and not exposed to other components.
	StorePath string
}

// Store manages the permanent storage of private write sets for a ledger
type Store struct {
	db              *leveldbhelper.DBHandle
	ledgerid        string
	btlPolicy       pvtdatapolicy.BTLPolicy
	batchesInterval int
	maxBatchSize    int
	purgeInterval   uint64

	isEmpty            bool
	lastCommittedBlock uint64
	purgerLock         sync.Mutex
	collElgProcSync    *collElgProcSync
	// After committing the pvtdata of old blocks,
	// the `isLastUpdatedOldBlocksSet` is set to true.
	// Once the stateDB is updated with these pvtdata,
	// the `isLastUpdatedOldBlocksSet` is set to false.
	// isLastUpdatedOldBlocksSet is mainly used during the
	// recovery process. During the peer startup, if the
	// isLastUpdatedOldBlocksSet is set to true, the pvtdata
	// in the stateDB needs to be updated before finishing the
	// recovery operation.
	isLastUpdatedOldBlocksSet bool
}

type blkTranNumKey []byte

type dataEntry struct {
	key   *dataKey
	value *rwset.CollectionPvtReadWriteSet
}

type expiryEntry struct {
	key   *expiryKey
	value *ExpiryData
}

type expiryKey struct {
	expiringBlk   uint64
	committingBlk uint64
}

type nsCollBlk struct {
	ns, coll string
	blkNum   uint64
}

type dataKey struct {
	nsCollBlk
	txNum uint64
}

type missingDataKey struct {
	nsCollBlk
	isEligible bool
}

type storeEntries struct {
	dataEntries        []*dataEntry
	expiryEntries      []*expiryEntry
	missingDataEntries map[missingDataKey]*bitset.BitSet
}

// lastUpdatedOldBlocksList keeps the list of last updated blocks
// and is stored as the value of lastUpdatedOldBlocksKey (defined in kv_encoding.go)
type lastUpdatedOldBlocksList []uint64

type entriesForPvtDataOfOldBlocks struct {
	// for each <ns, coll, blkNum, txNum>, store the dataEntry, i.e., pvtData
	dataEntries map[dataKey]*rwset.CollectionPvtReadWriteSet
	// store the retrieved (& updated) expiryData in expiryEntries
	expiryEntries map[expiryKey]*ExpiryData
	// for each <ns, coll, blkNum>, store the retrieved (& updated) bitmap in the missingDataEntries
	missingDataEntries map[nsCollBlk]*bitset.BitSet
}

//////// Provider functions  /////////////
//////////////////////////////////////////

// NewProvider instantiates a StoreProvider
func NewProvider(conf *PrivateDataConfig) (*Provider, error) {
	dbProvider, err := leveldbhelper.NewProvider(&leveldbhelper.Conf{DBPath: conf.StorePath})
	if err != nil {
		return nil, err
	}
	return &Provider{
		dbProvider: dbProvider,
		pvtData:    conf,
	}, nil
}

// OpenStore returns a handle to a store
func (p *Provider) OpenStore(ledgerid string) (*Store, error) {
	dbHandle := p.dbProvider.GetDBHandle(ledgerid)
	s := &Store{
		db:              dbHandle,
		ledgerid:        ledgerid,
		batchesInterval: p.pvtData.BatchesInterval,
		maxBatchSize:    p.pvtData.MaxBatchSize,
		purgeInterval:   uint64(p.pvtData.PurgeInterval),
		collElgProcSync: &collElgProcSync{
			notification: make(chan bool, 1),
			procComplete: make(chan bool, 1),
		},
	}
	if err := s.initState(); err != nil {
		return nil, err
	}
	s.launchCollElgProc()
	logger.Debugf("Pvtdata store opened. Initial state: isEmpty [%t], lastCommittedBlock [%d]",
		s.isEmpty, s.lastCommittedBlock)
	return s, nil
}

// Close closes the store
func (p *Provider) Close() {
	p.dbProvider.Close()
}

//////// store functions  ////////////////
//////////////////////////////////////////

func (s *Store) initState() error {
	var err error
	var blist lastUpdatedOldBlocksList
	if s.isEmpty, s.lastCommittedBlock, err = s.getLastCommittedBlockNum(); err != nil {
		return err
	}

	// TODO: FAB-16298 -- the concept of pendingBatch is no longer valid
	// for pvtdataStore. We can remove it v2.1. We retain the concept in
	// v2.0 to allow rolling upgrade from v142 to v2.0
	batchPending, err := s.hasPendingCommit()
	if err != nil {
		return err
	}

	if batchPending {
		committingBlockNum := s.nextBlockNum()
		batch := leveldbhelper.NewUpdateBatch()
		batch.Put(lastCommittedBlkkey, encodeLastCommittedBlockVal(committingBlockNum))
		batch.Delete(pendingCommitKey)
		if err := s.db.WriteBatch(batch, true); err != nil {
			return err
		}
		s.isEmpty = false
		s.lastCommittedBlock = committingBlockNum
	}

	if blist, err = s.getLastUpdatedOldBlocksList(); err != nil {
		return err
	}
	if len(blist) > 0 {
		s.isLastUpdatedOldBlocksSet = true
	} // false if not set

	return nil
}

// Init initializes the store. This function is expected to be invoked before using the store
func (s *Store) Init(btlPolicy pvtdatapolicy.BTLPolicy) {
	s.btlPolicy = btlPolicy
}

// Commit commits the pvt data as well as both the eligible and ineligible
// missing private data --- `eligible` denotes that the missing private data belongs to a collection
// for which this peer is a member; `ineligible` denotes that the missing private data belong to a
// collection for which this peer is not a member.
func (s *Store) Commit(blockNum uint64, pvtData []*ledger.TxPvtData, missingPvtData ledger.TxMissingPvtDataMap) error {
	expectedBlockNum := s.nextBlockNum()
	if expectedBlockNum != blockNum {
		return &ErrIllegalArgs{fmt.Sprintf("Expected block number=%d, received block number=%d", expectedBlockNum, blockNum)}
	}

	batch := leveldbhelper.NewUpdateBatch()
	var err error
	var keyBytes, valBytes []byte

	storeEntries, err := prepareStoreEntries(blockNum, pvtData, s.btlPolicy, missingPvtData)
	if err != nil {
		return err
	}

	for _, dataEntry := range storeEntries.dataEntries {
		keyBytes = encodeDataKey(dataEntry.key)
		if valBytes, err = encodeDataValue(dataEntry.value); err != nil {
			return err
		}
		batch.Put(keyBytes, valBytes)
	}

	for _, expiryEntry := range storeEntries.expiryEntries {
		keyBytes = encodeExpiryKey(expiryEntry.key)
		if valBytes, err = encodeExpiryValue(expiryEntry.value); err != nil {
			return err
		}
		batch.Put(keyBytes, valBytes)
	}

	for missingDataKey, missingDataValue := range storeEntries.missingDataEntries {
		keyBytes = encodeMissingDataKey(&missingDataKey)
		if valBytes, err = encodeMissingDataValue(missingDataValue); err != nil {
			return err
		}
		batch.Put(keyBytes, valBytes)
	}

	committingBlockNum := s.nextBlockNum()
	logger.Debugf("Committing private data for block [%d]", committingBlockNum)
	batch.Put(lastCommittedBlkkey, encodeLastCommittedBlockVal(committingBlockNum))
	if err := s.db.WriteBatch(batch, true); err != nil {
		return err
	}

	s.isEmpty = false
	atomic.StoreUint64(&s.lastCommittedBlock, committingBlockNum)
	logger.Debugf("Committed private data for block [%d]", committingBlockNum)
	s.performPurgeIfScheduled(committingBlockNum)
	return nil
}

// CommitPvtDataOfOldBlocks commits the pvtData (i.e., previously missing data) of old blocks.
// The parameter `blocksPvtData` refers a list of old block's pvtdata which are missing in the pvtstore.
// Given a list of old block's pvtData, `CommitPvtDataOfOldBlocks` performs the following four
// operations
// (1) construct dataEntries for all pvtData
// (2) construct update entries (i.e., dataEntries, expiryEntries, missingDataEntries)
//     from the above created data entries
// (3) create a db update batch from the update entries
// (4) commit the update batch to the pvtStore
func (s *Store) CommitPvtDataOfOldBlocks(blocksPvtData map[uint64][]*ledger.TxPvtData) error {
	if s.isLastUpdatedOldBlocksSet {
		return &ErrIllegalCall{`The lastUpdatedOldBlocksList is set. It means that the
		stateDB may not be in sync with the pvtStore`}
	}

	// (1) construct dataEntries for all pvtData
	dataEntries := constructDataEntriesFromBlocksPvtData(blocksPvtData)

	// (2) construct update entries (i.e., dataEntries, expiryEntries, missingDataEntries) from the above created data entries
	logger.Debugf("Constructing pvtdatastore entries for pvtData of [%d] old blocks", len(blocksPvtData))
	updateEntries, err := s.constructUpdateEntriesFromDataEntries(dataEntries)
	if err != nil {
		return err
	}

	// (3) create a db update batch from the update entries
	logger.Debug("Constructing update batch from pvtdatastore entries")
	batch, err := constructUpdateBatchFromUpdateEntries(updateEntries)
	if err != nil {
		return err
	}

	// (4) commit the update batch to the pvtStore
	logger.Debug("Committing the update batch to pvtdatastore")
	if err := s.commitBatch(batch); err != nil {
		return err
	}

	return nil
}

func constructDataEntriesFromBlocksPvtData(blocksPvtData map[uint64][]*ledger.TxPvtData) []*dataEntry {
	// construct dataEntries for all pvtData
	var dataEntries []*dataEntry
	for blkNum, pvtData := range blocksPvtData {
		// prepare the dataEntries for the pvtData
		dataEntries = append(dataEntries, prepareDataEntries(blkNum, pvtData)...)
	}
	return dataEntries
}

func (s *Store) constructUpdateEntriesFromDataEntries(dataEntries []*dataEntry) (*entriesForPvtDataOfOldBlocks, error) {
	updateEntries := &entriesForPvtDataOfOldBlocks{
		dataEntries:        make(map[dataKey]*rwset.CollectionPvtReadWriteSet),
		expiryEntries:      make(map[expiryKey]*ExpiryData),
		missingDataEntries: make(map[nsCollBlk]*bitset.BitSet)}

	// for each data entry, first, get the expiryData and missingData from the pvtStore.
	// Second, update the expiryData and missingData as per the data entry. Finally, add
	// the data entry along with the updated expiryData and missingData to the update entries
	for _, dataEntry := range dataEntries {
		// get the expiryBlk number to construct the expiryKey
		expiryKey, err := s.constructExpiryKeyFromDataEntry(dataEntry)
		if err != nil {
			return nil, err
		}

		// get the existing expiryData entry
		var expiryData *ExpiryData
		if !neverExpires(expiryKey.expiringBlk) {
			if expiryData, err = s.getExpiryDataFromUpdateEntriesOrStore(updateEntries, expiryKey); err != nil {
				return nil, err
			}
			if expiryData == nil {
				// data entry is already expired
				// and purged (a rare scenario)
				continue
			}
		}

		// get the existing missingData entry
		var missingData *bitset.BitSet
		nsCollBlk := dataEntry.key.nsCollBlk
		if missingData, err = s.getMissingDataFromUpdateEntriesOrStore(updateEntries, nsCollBlk); err != nil {
			return nil, err
		}
		if missingData == nil {
			// data entry is already expired
			// and purged (a rare scenario)
			continue
		}

		updateEntries.addDataEntry(dataEntry)
		if expiryData != nil { // would be nil for the never expiring entry
			expiryEntry := &expiryEntry{&expiryKey, expiryData}
			updateEntries.updateAndAddExpiryEntry(expiryEntry, dataEntry.key)
		}
		updateEntries.updateAndAddMissingDataEntry(missingData, dataEntry.key)
	}
	return updateEntries, nil
}

func (s *Store) constructExpiryKeyFromDataEntry(dataEntry *dataEntry) (expiryKey, error) {
	// get the expiryBlk number to construct the expiryKey
	nsCollBlk := dataEntry.key.nsCollBlk
	expiringBlk, err := s.btlPolicy.GetExpiringBlock(nsCollBlk.ns, nsCollBlk.coll, nsCollBlk.blkNum)
	if err != nil {
		return expiryKey{}, err
	}
	return expiryKey{expiringBlk, nsCollBlk.blkNum}, nil
}

func (s *Store) getExpiryDataFromUpdateEntriesOrStore(updateEntries *entriesForPvtDataOfOldBlocks, expiryKey expiryKey) (*ExpiryData, error) {
	expiryData, ok := updateEntries.expiryEntries[expiryKey]
	if !ok {
		var err error
		expiryData, err = s.getExpiryDataOfExpiryKey(&expiryKey)
		if err != nil {
			return nil, err
		}
	}
	return expiryData, nil
}

func (s *Store) getMissingDataFromUpdateEntriesOrStore(updateEntries *entriesForPvtDataOfOldBlocks, nsCollBlk nsCollBlk) (*bitset.BitSet, error) {
	missingData, ok := updateEntries.missingDataEntries[nsCollBlk]
	if !ok {
		var err error
		missingDataKey := &missingDataKey{nsCollBlk, true}
		missingData, err = s.getBitmapOfMissingDataKey(missingDataKey)
		if err != nil {
			return nil, err
		}
	}
	return missingData, nil
}

func (updateEntries *entriesForPvtDataOfOldBlocks) addDataEntry(dataEntry *dataEntry) {
	dataKey := dataKey{dataEntry.key.nsCollBlk, dataEntry.key.txNum}
	updateEntries.dataEntries[dataKey] = dataEntry.value
}

func (updateEntries *entriesForPvtDataOfOldBlocks) updateAndAddExpiryEntry(expiryEntry *expiryEntry, dataKey *dataKey) {
	txNum := dataKey.txNum
	nsCollBlk := dataKey.nsCollBlk
	// update
	expiryEntry.value.addPresentData(nsCollBlk.ns, nsCollBlk.coll, txNum)
	// we cannot delete entries from MissingDataMap as
	// we keep only one entry per missing <ns-col>
	// irrespective of the number of txNum.

	// add
	expiryKey := expiryKey{expiryEntry.key.expiringBlk, expiryEntry.key.committingBlk}
	updateEntries.expiryEntries[expiryKey] = expiryEntry.value
}

func (updateEntries *entriesForPvtDataOfOldBlocks) updateAndAddMissingDataEntry(missingData *bitset.BitSet, dataKey *dataKey) {

	txNum := dataKey.txNum
	nsCollBlk := dataKey.nsCollBlk
	// update
	missingData.Clear(uint(txNum))
	// add
	updateEntries.missingDataEntries[nsCollBlk] = missingData
}

func constructUpdateBatchFromUpdateEntries(updateEntries *entriesForPvtDataOfOldBlocks) (*leveldbhelper.UpdateBatch, error) {
	batch := leveldbhelper.NewUpdateBatch()

	// add the following four types of entries to the update batch: (1) new data entries
	// (i.e., pvtData), (2) updated expiry entries, (3) updated missing data entries, and
	// (4) updated block list

	// (1) add new data entries to the batch
	if err := addNewDataEntriesToUpdateBatch(batch, updateEntries); err != nil {
		return nil, err
	}

	// (2) add updated expiryEntry to the batch
	if err := addUpdatedExpiryEntriesToUpdateBatch(batch, updateEntries); err != nil {
		return nil, err
	}

	// (3) add updated missingData to the batch
	if err := addUpdatedMissingDataEntriesToUpdateBatch(batch, updateEntries); err != nil {
		return nil, err
	}

	return batch, nil
}

func addNewDataEntriesToUpdateBatch(batch *leveldbhelper.UpdateBatch, entries *entriesForPvtDataOfOldBlocks) error {
	var keyBytes, valBytes []byte
	var err error
	for dataKey, pvtData := range entries.dataEntries {
		keyBytes = encodeDataKey(&dataKey)
		if valBytes, err = encodeDataValue(pvtData); err != nil {
			return err
		}
		batch.Put(keyBytes, valBytes)
	}
	return nil
}

func addUpdatedExpiryEntriesToUpdateBatch(batch *leveldbhelper.UpdateBatch, entries *entriesForPvtDataOfOldBlocks) error {
	var keyBytes, valBytes []byte
	var err error
	for expiryKey, expiryData := range entries.expiryEntries {
		keyBytes = encodeExpiryKey(&expiryKey)
		if valBytes, err = encodeExpiryValue(expiryData); err != nil {
			return err
		}
		batch.Put(keyBytes, valBytes)
	}
	return nil
}

func addUpdatedMissingDataEntriesToUpdateBatch(batch *leveldbhelper.UpdateBatch, entries *entriesForPvtDataOfOldBlocks) error {
	var keyBytes, valBytes []byte
	var err error
	for nsCollBlk, missingData := range entries.missingDataEntries {
		keyBytes = encodeMissingDataKey(&missingDataKey{nsCollBlk, true})
		// if the missingData is empty, we need to delete the missingDataKey
		if missingData.None() {
			batch.Delete(keyBytes)
			continue
		}
		if valBytes, err = encodeMissingDataValue(missingData); err != nil {
			return err
		}
		batch.Put(keyBytes, valBytes)
	}
	return nil
}

func (s *Store) commitBatch(batch *leveldbhelper.UpdateBatch) error {
	// commit the batch to the store
	if err := s.db.WriteBatch(batch, true); err != nil {
		return err
	}

	return nil
}

// TODO FAB-16293 -- GetLastUpdatedOldBlocksPvtData() can be removed either in v2.0 or in v2.1.
// If we decide to rebuild stateDB in v2.0, by default, the rebuild logic would take
// care of synching stateDB with pvtdataStore without calling GetLastUpdatedOldBlocksPvtData().
// Hence, it can be safely removed. Suppose if we decide not to rebuild stateDB in v2.0,
// we can remove this function in v2.1.
// GetLastUpdatedOldBlocksPvtData returns the pvtdata of blocks listed in `lastUpdatedOldBlocksList`
func (s *Store) GetLastUpdatedOldBlocksPvtData() (map[uint64][]*ledger.TxPvtData, error) {
	if !s.isLastUpdatedOldBlocksSet {
		return nil, nil
	}

	updatedBlksList, err := s.getLastUpdatedOldBlocksList()
	if err != nil {
		return nil, err
	}

	blksPvtData := make(map[uint64][]*ledger.TxPvtData)
	for _, blkNum := range updatedBlksList {
		if blksPvtData[blkNum], err = s.GetPvtDataByBlockNum(blkNum, nil); err != nil {
			return nil, err
		}
	}
	return blksPvtData, nil
}

func (s *Store) getLastUpdatedOldBlocksList() ([]uint64, error) {
	var v []byte
	var err error
	if v, err = s.db.Get(lastUpdatedOldBlocksKey); err != nil {
		return nil, err
	}
	if v == nil {
		return nil, nil
	}

	var updatedBlksList []uint64
	buf := proto.NewBuffer(v)
	numBlks, err := buf.DecodeVarint()
	if err != nil {
		return nil, err
	}
	for i := 0; i < int(numBlks); i++ {
		blkNum, err := buf.DecodeVarint()
		if err != nil {
			return nil, err
		}
		updatedBlksList = append(updatedBlksList, blkNum)
	}
	return updatedBlksList, nil
}

// TODO FAB-16294 -- ResetLastUpdatedOldBlocksList() can be removed in v2.1.
// From v2.0 onwards, we do not store the last updatedBlksList. Only to support
// the rolling upgrade from v142 to v2.0, we retain the ResetLastUpdatedOldBlocksList()
// in v2.0.

// ResetLastUpdatedOldBlocksList removes the `lastUpdatedOldBlocksList` entry from the store
func (s *Store) ResetLastUpdatedOldBlocksList() error {
	batch := leveldbhelper.NewUpdateBatch()
	batch.Delete(lastUpdatedOldBlocksKey)
	if err := s.db.WriteBatch(batch, true); err != nil {
		return err
	}
	s.isLastUpdatedOldBlocksSet = false
	return nil
}

// GetPvtDataByBlockNum returns only the pvt data  corresponding to the given block number
// The pvt data is filtered by the list of 'ns/collections' supplied in the filter
// A nil filter does not filter any results
func (s *Store) GetPvtDataByBlockNum(blockNum uint64, filter ledger.PvtNsCollFilter) ([]*ledger.TxPvtData, error) {
	logger.Debugf("Get private data for block [%d], filter=%#v", blockNum, filter)
	if s.isEmpty {
		return nil, &ErrOutOfRange{"The store is empty"}
	}
	lastCommittedBlock := atomic.LoadUint64(&s.lastCommittedBlock)
	if blockNum > lastCommittedBlock {
		return nil, &ErrOutOfRange{fmt.Sprintf("Last committed block=%d, block requested=%d", lastCommittedBlock, blockNum)}
	}
	startKey, endKey := getDataKeysForRangeScanByBlockNum(blockNum)
	logger.Debugf("Querying private data storage for write sets using startKey=%#v, endKey=%#v", startKey, endKey)
	itr := s.db.GetIterator(startKey, endKey)
	defer itr.Release()

	var blockPvtdata []*ledger.TxPvtData
	var currentTxNum uint64
	var currentTxWsetAssember *txPvtdataAssembler
	firstItr := true

	for itr.Next() {
		dataKeyBytes := itr.Key()
		v11Fmt, err := v11Format(dataKeyBytes)
		if err != nil {
			return nil, err
		}
		if v11Fmt {
			return v11RetrievePvtdata(itr, filter)
		}
		dataValueBytes := itr.Value()
		dataKey, err := decodeDatakey(dataKeyBytes)
		if err != nil {
			return nil, err
		}
		expired, err := isExpired(dataKey.nsCollBlk, s.btlPolicy, lastCommittedBlock)
		if err != nil {
			return nil, err
		}
		if expired || !passesFilter(dataKey, filter) {
			continue
		}
		dataValue, err := decodeDataValue(dataValueBytes)
		if err != nil {
			return nil, err
		}

		if firstItr {
			currentTxNum = dataKey.txNum
			currentTxWsetAssember = newTxPvtdataAssembler(blockNum, currentTxNum)
			firstItr = false
		}

		if dataKey.txNum != currentTxNum {
			blockPvtdata = append(blockPvtdata, currentTxWsetAssember.getTxPvtdata())
			currentTxNum = dataKey.txNum
			currentTxWsetAssember = newTxPvtdataAssembler(blockNum, currentTxNum)
		}
		currentTxWsetAssember.add(dataKey.ns, dataValue)
	}
	if currentTxWsetAssember != nil {
		blockPvtdata = append(blockPvtdata, currentTxWsetAssember.getTxPvtdata())
	}
	return blockPvtdata, nil
}

// GetMissingPvtDataInfoForMostRecentBlocks returns the missing private data information for the
// most recent `maxBlock` blocks which miss at least a private data of a eligible collection.
func (s *Store) GetMissingPvtDataInfoForMostRecentBlocks(maxBlock int) (ledger.MissingPvtDataInfo, error) {
	// we assume that this function would be called by the gossip only after processing the
	// last retrieved missing pvtdata info and committing the same.
	if maxBlock < 1 {
		return nil, nil
	}

	missingPvtDataInfo := make(ledger.MissingPvtDataInfo)
	numberOfBlockProcessed := 0
	lastProcessedBlock := uint64(0)
	isMaxBlockLimitReached := false
	// as we are not acquiring a read lock, new blocks can get committed while we
	// construct the MissingPvtDataInfo. As a result, lastCommittedBlock can get
	// changed. To ensure consistency, we atomically load the lastCommittedBlock value
	lastCommittedBlock := atomic.LoadUint64(&s.lastCommittedBlock)

	startKey, endKey := createRangeScanKeysForEligibleMissingDataEntries(lastCommittedBlock)
	dbItr := s.db.GetIterator(startKey, endKey)
	defer dbItr.Release()

	for dbItr.Next() {
		missingDataKeyBytes := dbItr.Key()
		missingDataKey := decodeMissingDataKey(missingDataKeyBytes)

		if isMaxBlockLimitReached && (missingDataKey.blkNum != lastProcessedBlock) {
			// ensures that exactly maxBlock number
			// of blocks' entries are processed
			break
		}

		// check whether the entry is expired. If so, move to the next item.
		// As we may use the old lastCommittedBlock value, there is a possibility that
		// this missing data is actually expired but we may get the stale information.
		// Though it may leads to extra work of pulling the expired data, it will not
		// affect the correctness. Further, as we try to fetch the most recent missing
		// data (less possibility of expiring now), such scenario would be rare. In the
		// best case, we can load the latest lastCommittedBlock value here atomically to
		// make this scenario very rare.
		lastCommittedBlock = atomic.LoadUint64(&s.lastCommittedBlock)
		expired, err := isExpired(missingDataKey.nsCollBlk, s.btlPolicy, lastCommittedBlock)
		if err != nil {
			return nil, err
		}
		if expired {
			continue
		}

		// check for an existing entry for the blkNum in the MissingPvtDataInfo.
		// If no such entry exists, create one. Also, keep track of the number of
		// processed block due to maxBlock limit.
		if _, ok := missingPvtDataInfo[missingDataKey.blkNum]; !ok {
			numberOfBlockProcessed++
			if numberOfBlockProcessed == maxBlock {
				isMaxBlockLimitReached = true
				// as there can be more than one entry for this block,
				// we cannot `break` here
				lastProcessedBlock = missingDataKey.blkNum
			}
		}

		valueBytes := dbItr.Value()
		bitmap, err := decodeMissingDataValue(valueBytes)
		if err != nil {
			return nil, err
		}

		// for each transaction which misses private data, make an entry in missingBlockPvtDataInfo
		for index, isSet := bitmap.NextSet(0); isSet; index, isSet = bitmap.NextSet(index + 1) {
			txNum := uint64(index)
			missingPvtDataInfo.Add(missingDataKey.blkNum, txNum, missingDataKey.ns, missingDataKey.coll)
		}
	}

	return missingPvtDataInfo, nil
}

// ProcessCollsEligibilityEnabled notifies the store when the peer becomes eligible to receive data for an
// existing collection. Parameter 'committingBlk' refers to the block number that contains the corresponding
// collection upgrade transaction and the parameter 'nsCollMap' contains the collections for which the peer
// is now eligible to receive pvt data
func (s *Store) ProcessCollsEligibilityEnabled(committingBlk uint64, nsCollMap map[string][]string) error {
	key := encodeCollElgKey(committingBlk)
	m := newCollElgInfo(nsCollMap)
	val, err := encodeCollElgVal(m)
	if err != nil {
		return err
	}
	batch := leveldbhelper.NewUpdateBatch()
	batch.Put(key, val)
	if err = s.db.WriteBatch(batch, true); err != nil {
		return err
	}
	s.collElgProcSync.notify()
	return nil
}

func (s *Store) performPurgeIfScheduled(latestCommittedBlk uint64) {
	if latestCommittedBlk%s.purgeInterval != 0 {
		return
	}
	go func() {
		s.purgerLock.Lock()
		logger.Debugf("Purger started: Purging expired private data till block number [%d]", latestCommittedBlk)
		defer s.purgerLock.Unlock()
		err := s.purgeExpiredData(0, latestCommittedBlk)
		if err != nil {
			logger.Warningf("Could not purge data from pvtdata store:%s", err)
		}
		logger.Debug("Purger finished")
	}()
}

func (s *Store) purgeExpiredData(minBlkNum, maxBlkNum uint64) error {
	batch := leveldbhelper.NewUpdateBatch()
	expiryEntries, err := s.retrieveExpiryEntries(minBlkNum, maxBlkNum)
	if err != nil || len(expiryEntries) == 0 {
		return err
	}
	for _, expiryEntry := range expiryEntries {
		// this encoding could have been saved if the function retrieveExpiryEntries also returns the encoded expiry keys.
		// However, keeping it for better readability
		batch.Delete(encodeExpiryKey(expiryEntry.key))
		dataKeys, missingDataKeys := deriveKeys(expiryEntry)
		for _, dataKey := range dataKeys {
			batch.Delete(encodeDataKey(dataKey))
		}
		for _, missingDataKey := range missingDataKeys {
			batch.Delete(encodeMissingDataKey(missingDataKey))
		}
		s.db.WriteBatch(batch, false)
	}
	logger.Infof("[%s] - [%d] Entries purged from private data storage till block number [%d]", s.ledgerid, len(expiryEntries), maxBlkNum)
	return nil
}

func (s *Store) retrieveExpiryEntries(minBlkNum, maxBlkNum uint64) ([]*expiryEntry, error) {
	startKey, endKey := getExpiryKeysForRangeScan(minBlkNum, maxBlkNum)
	logger.Debugf("retrieveExpiryEntries(): startKey=%#v, endKey=%#v", startKey, endKey)
	itr := s.db.GetIterator(startKey, endKey)
	defer itr.Release()

	var expiryEntries []*expiryEntry
	for itr.Next() {
		expiryKeyBytes := itr.Key()
		expiryValueBytes := itr.Value()
		expiryKey, err := decodeExpiryKey(expiryKeyBytes)
		if err != nil {
			return nil, err
		}
		expiryValue, err := decodeExpiryValue(expiryValueBytes)
		if err != nil {
			return nil, err
		}
		expiryEntries = append(expiryEntries, &expiryEntry{key: expiryKey, value: expiryValue})
	}
	return expiryEntries, nil
}

func (s *Store) launchCollElgProc() {
	go func() {
		s.processCollElgEvents() // process collection eligibility events when store is opened - in case there is an unprocessed events from previous run
		for {
			logger.Debugf("Waiting for collection eligibility event")
			s.collElgProcSync.waitForNotification()
			s.processCollElgEvents()
			s.collElgProcSync.done()
		}
	}()
}

func (s *Store) processCollElgEvents() {
	logger.Debugf("Starting to process collection eligibility events")
	s.purgerLock.Lock()
	defer s.purgerLock.Unlock()
	collElgStartKey, collElgEndKey := createRangeScanKeysForCollElg()
	eventItr := s.db.GetIterator(collElgStartKey, collElgEndKey)
	defer eventItr.Release()
	batch := leveldbhelper.NewUpdateBatch()
	totalEntriesConverted := 0

	for eventItr.Next() {
		collElgKey, collElgVal := eventItr.Key(), eventItr.Value()
		blkNum := decodeCollElgKey(collElgKey)
		CollElgInfo, err := decodeCollElgVal(collElgVal)
		logger.Debugf("Processing collection eligibility event [blkNum=%d], CollElgInfo=%s", blkNum, CollElgInfo)
		if err != nil {
			logger.Errorf("This error is not expected %s", err)
			continue
		}
		for ns, colls := range CollElgInfo.NsCollMap {
			var coll string
			for _, coll = range colls.Entries {
				logger.Infof("Converting missing data entries from ineligible to eligible for [ns=%s, coll=%s]", ns, coll)
				startKey, endKey := createRangeScanKeysForIneligibleMissingData(blkNum, ns, coll)
				collItr := s.db.GetIterator(startKey, endKey)
				collEntriesConverted := 0

				for collItr.Next() { // each entry
					originalKey, originalVal := collItr.Key(), collItr.Value()
					modifiedKey := decodeMissingDataKey(originalKey)
					modifiedKey.isEligible = true
					batch.Delete(originalKey)
					copyVal := make([]byte, len(originalVal))
					copy(copyVal, originalVal)
					batch.Put(encodeMissingDataKey(modifiedKey), copyVal)
					collEntriesConverted++
					if batch.Len() > s.maxBatchSize {
						s.db.WriteBatch(batch, true)
						batch = leveldbhelper.NewUpdateBatch()
						sleepTime := time.Duration(s.batchesInterval)
						logger.Infof("Going to sleep for %d milliseconds between batches. Entries for [ns=%s, coll=%s] converted so far = %d",
							sleepTime, ns, coll, collEntriesConverted)
						s.purgerLock.Unlock()
						time.Sleep(sleepTime * time.Millisecond)
						s.purgerLock.Lock()
					}
				} // entry loop

				collItr.Release()
				logger.Infof("Converted all [%d] entries for [ns=%s, coll=%s]", collEntriesConverted, ns, coll)
				totalEntriesConverted += collEntriesConverted
			} // coll loop
		} // ns loop
		batch.Delete(collElgKey) // delete the collection eligibility event key as well
	} // event loop

	s.db.WriteBatch(batch, true)
	logger.Debugf("Converted [%d] ineligible missing data entries to eligible", totalEntriesConverted)
}

// LastCommittedBlockHeight returns the height of the last committed block
func (s *Store) LastCommittedBlockHeight() (uint64, error) {
	if s.isEmpty {
		return 0, nil
	}
	return atomic.LoadUint64(&s.lastCommittedBlock) + 1, nil
}

func (s *Store) nextBlockNum() uint64 {
	if s.isEmpty {
		return 0
	}
	return atomic.LoadUint64(&s.lastCommittedBlock) + 1
}

// TODO: FAB-16298 -- the concept of pendingBatch is no longer valid
// for pvtdataStore. We can remove it v2.1. We retain the concept in
// v2.0 to allow rolling upgrade from v142 to v2.0
func (s *Store) hasPendingCommit() (bool, error) {
	var v []byte
	var err error
	if v, err = s.db.Get(pendingCommitKey); err != nil {
		return false, err
	}
	return v != nil, nil
}

func (s *Store) getLastCommittedBlockNum() (bool, uint64, error) {
	var v []byte
	var err error
	if v, err = s.db.Get(lastCommittedBlkkey); v == nil || err != nil {
		return true, 0, err
	}
	return false, decodeLastCommittedBlockVal(v), nil
}

type collElgProcSync struct {
	notification, procComplete chan bool
}

func (sync *collElgProcSync) notify() {
	select {
	case sync.notification <- true:
		logger.Debugf("Signaled to collection eligibility processing routine")
	default: //noop
		logger.Debugf("Previous signal still pending. Skipping new signal")
	}
}

func (sync *collElgProcSync) waitForNotification() {
	<-sync.notification
}

func (sync *collElgProcSync) done() {
	select {
	case sync.procComplete <- true:
	default:
	}
}

func (sync *collElgProcSync) waitForDone() {
	<-sync.procComplete
}

func (s *Store) getBitmapOfMissingDataKey(missingDataKey *missingDataKey) (*bitset.BitSet, error) {
	var v []byte
	var err error
	if v, err = s.db.Get(encodeMissingDataKey(missingDataKey)); err != nil {
		return nil, err
	}
	if v == nil {
		return nil, nil
	}
	return decodeMissingDataValue(v)
}

func (s *Store) getExpiryDataOfExpiryKey(expiryKey *expiryKey) (*ExpiryData, error) {
	var v []byte
	var err error
	if v, err = s.db.Get(encodeExpiryKey(expiryKey)); err != nil {
		return nil, err
	}
	if v == nil {
		return nil, nil
	}
	return decodeExpiryValue(v)
}

// ErrIllegalCall is to be thrown by a store impl if the store does not expect a call to Prepare/Commit/Rollback/InitLastCommittedBlock
type ErrIllegalCall struct {
	msg string
}

func (err *ErrIllegalCall) Error() string {
	return err.msg
}

// ErrIllegalArgs is to be thrown by a store impl if the args passed are not allowed
type ErrIllegalArgs struct {
	msg string
}

func (err *ErrIllegalArgs) Error() string {
	return err.msg
}

// ErrOutOfRange is to be thrown for the request for the data that is not yet committed
type ErrOutOfRange struct {
	msg string
}

func (err *ErrOutOfRange) Error() string {
	return err.msg
}
