/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package blkstorage

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/hyperledger/fabric/common/ledger/testutil"
	"github.com/hyperledger/fabric/common/ledger/util"
	"github.com/golang/protobuf/proto"
	"github.com/stretchr/testify/assert"
)

func TestConstructCheckpointInfoFromBlockFiles(t *testing.T) {
	ledgerid := "testLedger"
	conf := NewConf(testPath(), 0)
	blkStoreDir := conf.getLedgerBlockDir(ledgerid)
	env := newTestEnv(t, conf)
	util.CreateDirIfMissing(blkStoreDir)
	defer env.Cleanup()

	// checkpoint constructed on an empty block folder should return CPInfo with isChainEmpty: true
	cpInfo, err := constructCheckpointInfoFromBlockFiles(blkStoreDir)
	assert.NoError(t, err)
	assert.Equal(t, &checkpointInfo{isChainEmpty: true, lastBlockNumber: 0, latestFileChunksize: 0, latestFileChunkSuffixNum: 0}, cpInfo)

	w := newTestBlockfileWrapper(env, ledgerid)
	defer w.close()
	blockfileMgr := w.blockfileMgr
	bg, gb := testutil.NewBlockGenerator(t, ledgerid, false)

	// Add a few blocks and verify that cpinfo derived from filesystem should be same as from the blockfile manager
	blockfileMgr.addBlock(gb)
	for _, blk := range bg.NextTestBlocks(3) {
		blockfileMgr.addBlock(blk)
	}
	checkCPInfoFromFile(t, blkStoreDir, blockfileMgr.cpInfo)

	// Move the chain to new file and check cpinfo derived from file system
	blockfileMgr.moveToNextFile()
	checkCPInfoFromFile(t, blkStoreDir, blockfileMgr.cpInfo)

	// Add a few blocks that would go to new file and verify that cpinfo derived from filesystem should be same as from the blockfile manager
	for _, blk := range bg.NextTestBlocks(3) {
		blockfileMgr.addBlock(blk)
	}
	checkCPInfoFromFile(t, blkStoreDir, blockfileMgr.cpInfo)

	// Write a partial block (to simulate a crash) and verify that cpinfo derived from filesystem should be same as from the blockfile manager
	lastTestBlk := bg.NextTestBlocks(1)[0]
	blockBytes, _, err := serializeBlock(lastTestBlk)
	assert.NoError(t, err)
	partialByte := append(proto.EncodeVarint(uint64(len(blockBytes))), blockBytes[len(blockBytes)/2:]...)
	blockfileMgr.currentFileWriter.append(partialByte, true)
	checkCPInfoFromFile(t, blkStoreDir, blockfileMgr.cpInfo)

	// Close the block storage, drop the index and restart and verify
	cpInfoBeforeClose := blockfileMgr.cpInfo
	w.close()
	env.provider.Close()
	indexFolder := conf.getIndexDir()
	assert.NoError(t, os.RemoveAll(indexFolder))

	env = newTestEnv(t, conf)
	w = newTestBlockfileWrapper(env, ledgerid)
	blockfileMgr = w.blockfileMgr
	assert.Equal(t, cpInfoBeforeClose, blockfileMgr.cpInfo)

	lastBlkIndexed, err := blockfileMgr.index.getLastBlockIndexed()
	assert.NoError(t, err)
	assert.Equal(t, uint64(6), lastBlkIndexed)

	// Add the last block again after start and check cpinfo again
	assert.NoError(t, blockfileMgr.addBlock(lastTestBlk))
	checkCPInfoFromFile(t, blkStoreDir, blockfileMgr.cpInfo)
}

func TestBinarySearchBlockFileNum(t *testing.T) {
	blockStoreRootDir := testPath()
	blocks := testutil.ConstructTestBlocks(t, 100)
	maxFileSie := int(0.1 * float64(testutilEstimateTotalSizeOnDisk(t, blocks)))
	env := newTestEnv(t, NewConf(blockStoreRootDir, maxFileSie))
	defer env.Cleanup()
	blkfileMgrWrapper := newTestBlockfileWrapper(env, "testLedger")
	blkfileMgr := blkfileMgrWrapper.blockfileMgr

	blkfileMgrWrapper.addBlocks(blocks)

	ledgerDir := (&Conf{blockStorageDir: blockStoreRootDir}).getLedgerBlockDir("testLedger")
	files, err := ioutil.ReadDir(ledgerDir)
	assert.NoError(t, err)
	assert.Len(t, files, 11)

	for i := uint64(0); i < 100; i++ {
		fileNum, err := binarySearchFileNumForBlock(ledgerDir, i)
		assert.NoError(t, err)
		locFromIndex, err := blkfileMgr.index.getBlockLocByBlockNum(i)
		assert.NoError(t, err)
		expectedFileNum := locFromIndex.fileSuffixNum
		assert.Equal(t, expectedFileNum, fileNum)
	}
}

func checkCPInfoFromFile(t *testing.T, blkStoreDir string, expectedCPInfo *checkpointInfo) {
	cpInfo, err := constructCheckpointInfoFromBlockFiles(blkStoreDir)
	assert.NoError(t, err)
	assert.Equal(t, expectedCPInfo, cpInfo)
}
