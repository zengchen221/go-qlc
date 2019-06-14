package consensus

import (
	"github.com/qlcchain/go-qlc/common"
	"github.com/qlcchain/go-qlc/common/event"
	"github.com/qlcchain/go-qlc/common/types"
	"github.com/qlcchain/go-qlc/log"
	"github.com/qlcchain/go-qlc/p2p"
	"github.com/qlcchain/go-qlc/p2p/protos"
	"go.uber.org/zap"
	"math/big"
	"math/rand"
	"sort"
	"sync"
	"time"
)

const (
	minPovSyncPeerCount = 1
	checkPeerStatusTime = 30
	waitEnoughPeerTime  = 75
	maxSyncBlockPerReq  = 1000

	peerStatusInit = 0
	peerStatusGood = 1
	peerStatusBad  = 2
)

type PovSyncPeer struct {
	peerID         string
	currentHeight  uint64
	currentTD      *big.Int
	lastStatusTime time.Time
	status         int
}

type PovSyncPeerSetByTD []*PovSyncPeer

func (s PovSyncPeerSetByTD) Len() int           { return len(s) }
func (s PovSyncPeerSetByTD) Less(i, j int) bool { return s[i].currentTD.Cmp(s[j].currentTD) < 0 }
func (s PovSyncPeerSetByTD) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

type PovSyncPeerSetByHeight []*PovSyncPeer

func (s PovSyncPeerSetByHeight) Len() int           { return len(s) }
func (s PovSyncPeerSetByHeight) Less(i, j int) bool { return s[i].currentHeight < s[j].currentHeight }
func (s PovSyncPeerSetByHeight) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

type PovSyncer struct {
	povEngine *PoVEngine
	logger    *zap.SugaredLogger
	allPeers  sync.Map // map[string]*PovSyncPeer

	state         common.SyncState
	fromHeight    uint64
	toHeight      uint64
	currentHeight uint64
	lastCheckTime time.Time

	syncHeight uint64
	syncPeerID string

	syncPeerLostTime    time.Time
	lastSyncRequestTime time.Time

	messageCh chan *PovSyncMessage
	eventCh   chan *PovSyncEvent
	quitCh    chan struct{}
}

type PovSyncMessage struct {
	msgValue interface{}
	msgHash  types.Hash
	msgPeer  string
}

type PovSyncEvent struct {
	eventType common.TopicType
	eventData interface{}
}

func NewPovSyncer(povEngine *PoVEngine) *PovSyncer {
	ss := &PovSyncer{
		povEngine:     povEngine,
		state:         common.SyncNotStart,
		lastCheckTime: time.Now(),
		messageCh:     make(chan *PovSyncMessage, 100),
		eventCh:       make(chan *PovSyncEvent, 10),
		quitCh:        make(chan struct{}),
		logger:        log.NewLogger("pov_sync"),
	}
	return ss
}

func (ss *PovSyncer) Start() {
	eb := ss.povEngine.GetEventBus()
	if eb != nil {
		err := eb.SubscribeSync(string(common.EventAddP2PStream), ss.onAddP2PStream)
		if err != nil {
			return
		}
		err = eb.SubscribeSync(string(common.EventDeleteP2PStream), ss.onDeleteP2PStream)
		if err != nil {
			return
		}
		err = eb.SubscribeSync(string(common.EventPovPeerStatus), ss.onPovStatus)
		if err != nil {
			return
		}
		err = eb.Subscribe(string(common.EventPovBulkPullReq), ss.onPovBulkPullReq)
		if err != nil {
			return
		}
		err = eb.Subscribe(string(common.EventPovBulkPullRsp), ss.onPovBulkPullRsp)
		if err != nil {
			return
		}
	}

	common.Go(ss.mainLoop)
	common.Go(ss.syncLoop)
}

func (ss *PovSyncer) Stop() {
	eb := ss.povEngine.GetEventBus()
	if eb != nil {
		err := eb.Unsubscribe(string(common.EventAddP2PStream), ss.onAddP2PStream)
		if err != nil {
			return
		}
		err = eb.Unsubscribe(string(common.EventDeleteP2PStream), ss.onDeleteP2PStream)
		if err != nil {
			return
		}
		err = eb.Unsubscribe(string(common.EventPovPeerStatus), ss.onPovStatus)
		if err != nil {
			return
		}
		err = eb.Unsubscribe(string(common.EventPovBulkPullReq), ss.onPovBulkPullReq)
		if err != nil {
			return
		}
		err = eb.Unsubscribe(string(common.EventPovBulkPullRsp), ss.onPovBulkPullRsp)
		if err != nil {
			return
		}
	}

	close(ss.quitCh)
}

func (ss *PovSyncer) getChain() *PovBlockChain {
	return ss.povEngine.GetChain()
}

func (ss *PovSyncer) getEventBus() event.EventBus {
	return ss.povEngine.eb
}

func (ss *PovSyncer) getState() common.SyncState {
	return ss.state
}

func (ss *PovSyncer) mainLoop() {
	checkPeerTicker := time.NewTicker(checkPeerStatusTime * time.Second)

	for {
		select {
		case <-ss.quitCh:
			return

		case <-checkPeerTicker.C:
			ss.checkAllPeers()

		case msg := <-ss.messageCh:
			ss.processMessage(msg)

		case event := <-ss.eventCh:
			ss.processEvent(event)
		}
	}
}

func (ss *PovSyncer) syncLoop() {
	waitTimer := time.NewTimer(waitEnoughPeerTime * time.Second)

wait:
	for {
		select {
		case <-ss.quitCh:
			return
		case <-waitTimer.C:
			break wait
		default:
			if ss.PeerCount() >= minPovSyncPeerCount {
				break wait
			} else {
				time.Sleep(1 * time.Second)
			}
		}
	}

	waitTimer.Stop()

	checkSyncTicker := time.NewTicker(10 * time.Second)
	checkChainTicker := time.NewTicker(1 * time.Second)

loop:
	for {
		select {
		case <-ss.quitCh:
			return

		case <-checkSyncTicker.C:
			ss.checkSyncPeer()
			if ss.isFinished() {
				break loop
			}

		case <-checkChainTicker.C:
			ss.checkChain()
			if ss.isFinished() {
				break loop
			}
		}
	}

	ss.logger.Infof("exit sync loop")
	checkSyncTicker.Stop()
	checkChainTicker.Stop()
}

func (ss *PovSyncer) onAddP2PStream(peerID string) {
	ss.logger.Infof("add peer %s", peerID)

	peer := &PovSyncPeer{
		peerID:         peerID,
		currentHeight:  0,
		currentTD:      big.NewInt(0),
		lastStatusTime: time.Time{},
		status:         peerStatusInit,
	}

	ss.allPeers.Store(peerID, peer)

	ss.eventCh <- &PovSyncEvent{eventType: common.EventAddP2PStream, eventData: peerID}
}

func (ss *PovSyncer) onDeleteP2PStream(peerID string) {
	ss.logger.Infof("delete peer %s", peerID)
	ss.allPeers.Delete(peerID)

	ss.eventCh <- &PovSyncEvent{eventType: common.EventDeleteP2PStream, eventData: peerID}
}

func (ss *PovSyncer) onPovStatus(status *protos.PovStatus, msgHash types.Hash, msgPeer string) {
	if v, ok := ss.allPeers.Load(msgPeer); ok {
		peer := v.(*PovSyncPeer)

		td := new(big.Int).SetBytes(status.CurrentTD)
		ss.logger.Infof("recv PovStatus from peer %s, head %d/%s, td %d/%s",
			msgPeer, status.CurrentHeight, status.CurrentHash, td.BitLen(), td.Text(16))
		if status.GenesisHash != ss.getChain().GenesisBlock().GetHash() {
			ss.logger.Warnf("peer %s genesis hash %s is invalid", msgPeer, status.GenesisHash)
			return
		}

		peer.currentHeight = status.CurrentHeight
		peer.currentTD = td
		peer.lastStatusTime = time.Now()
		peer.status = peerStatusGood
	}
}

func (ss *PovSyncer) onPovBulkPullReq(req *protos.PovBulkPullReq, msgHash types.Hash, msgPeer string) {
	ss.messageCh <- &PovSyncMessage{msgValue: req, msgHash: msgHash, msgPeer: msgPeer}
}

func (ss *PovSyncer) onPovBulkPullRsp(rsp *protos.PovBulkPullRsp, msgHash types.Hash, msgPeer string) {
	ss.messageCh <- &PovSyncMessage{msgValue: rsp, msgHash: msgHash, msgPeer: msgPeer}
}

func (ss *PovSyncer) processMessage(msg *PovSyncMessage) {
	switch v := msg.msgValue.(type) {
	case *protos.PovBulkPullReq:
		ss.processPovBulkPullReq(msg)
	case *protos.PovBulkPullRsp:
		ss.processPovBulkPullRsp(msg)
	default:
		ss.logger.Infof("unknown message value type %T!\n", v)
	}
}

func (ss *PovSyncer) processPovBulkPullReq(msg *PovSyncMessage) {
	req := msg.msgValue.(*protos.PovBulkPullReq)

	if len(req.Locators) > 0 {
		ss.logger.Debugf("recv PovBulkPullReq from peer %s, reason %d locator %s count %d", msg.msgPeer, req.Reason, req.Locators[0], req.Count)
	} else if !req.StartHash.IsZero() {
		ss.logger.Debugf("recv PovBulkPullReq from peer %s, reason %d hash %s count %d", msg.msgPeer, req.Reason, req.StartHash, req.Count)
	} else {
		ss.logger.Debugf("recv PovBulkPullReq from peer %s, reason %d height %d count %d", msg.msgPeer, req.Reason, req.StartHeight, req.Count)
	}

	rsp := new(protos.PovBulkPullRsp)
	rsp.Reason = req.Reason

	startHeight := req.StartHeight
	blockCount := req.Count
	if len(req.Locators) > 0 {
		block := ss.getChain().LocateBestBlock(req.Locators)
		if block == nil {
			ss.logger.Infof("failed to locate best block %s", req.Locators[0])
			return
		}
		rsp.Blocks = append(rsp.Blocks, block)
		startHeight = block.GetHeight() + 1
		blockCount = blockCount - 1
	} else if !req.StartHash.IsZero() {
		block := ss.getChain().GetBlockByHash(req.StartHash)
		if block == nil {
			ss.logger.Infof("failed to get block %s", req.StartHash)
			return
		}
		rsp.Blocks = append(rsp.Blocks, block)
		startHeight = block.GetHeight() + 1
		blockCount = blockCount - 1
	}

	maxBlockSize := ss.povEngine.GetConfig().PoV.BlockSize
	curBlkMsgSize := 0

	endHeight := startHeight + uint64(blockCount)
	for height := startHeight; height < endHeight; height++ {
		block, err := ss.getChain().GetBlockByHeight(height)
		if err != nil {
			ss.logger.Infof("failed to get block %d, err %s", height, err)
			break
		}
		rsp.Blocks = append(rsp.Blocks, block)

		curBlkMsgSize = curBlkMsgSize + block.Msgsize()
		if curBlkMsgSize >= maxBlockSize {
			break
		}
	}

	rsp.Count = uint32(len(rsp.Blocks))

	ss.getEventBus().Publish(string(common.EventSendMsgToPeer), p2p.PovBulkPullRsp, rsp, msg.msgPeer)
}

func (ss *PovSyncer) processPovBulkPullRsp(msg *PovSyncMessage) {
	rsp := msg.msgValue.(*protos.PovBulkPullRsp)

	ss.logger.Debugf("recv PovBulkPullRsp from peer %s, reason %d count %d", msg.msgPeer, rsp.Reason, rsp.Count)

	if rsp.Count == 0 {
		return
	}

	if (rsp.Reason == protos.PovReasonSync) && (ss.getState() != common.Syncing) {
		return
	}

	fromType := types.PovBlockFromRemoteFetch
	if rsp.Reason == protos.PovReasonSync {
		fromType = types.PovBlockFromRemoteFetch
	}

	lastBlockHeight := uint64(0)
	for _, block := range rsp.Blocks {
		ss.povEngine.AddBlock(block, fromType)

		lastBlockHeight = block.GetHeight()
	}

	if rsp.Reason == protos.PovReasonSync {
		ss.requestSyncingBlocks(false, lastBlockHeight)
	}
}

func (ss *PovSyncer) processEvent(event *PovSyncEvent) {
	switch event.eventType {
	case common.EventAddP2PStream:
		ss.processStreamEvent(event)
	case common.EventDeleteP2PStream:
		break
	default:
		ss.logger.Infof("unknown event type %T!\n", event.eventType)
	}
}

func (ss *PovSyncer) processStreamEvent(event *PovSyncEvent) {
	peerID := event.eventData.(string)

	genesisBlock := ss.povEngine.chain.GenesisBlock()
	latestBlock := ss.povEngine.chain.LatestBlock()
	latestTD := ss.povEngine.chain.GetBlockTDByHash(latestBlock.GetHash())

	status := &protos.PovStatus{
		CurrentHeight: latestBlock.GetHeight(),
		CurrentTD:     latestTD.Bytes(),
		CurrentHash:   latestBlock.GetHash(),
		GenesisHash:   genesisBlock.GetHash(),
	}
	ss.logger.Debugf("send PovStatus to peer %s", peerID)
	ss.povEngine.eb.Publish(string(common.EventSendMsgToPeer), p2p.PovStatus, status, peerID)
}

func (ss *PovSyncer) checkAllPeers() {
	peerCount := ss.PeerCount()
	if peerCount <= 0 {
		return
	}

	genesisBlock := ss.povEngine.chain.GenesisBlock()
	latestBlock := ss.povEngine.chain.LatestBlock()
	latestTD := ss.povEngine.chain.GetBlockTDByHash(latestBlock.GetHash())

	status := &protos.PovStatus{
		CurrentHeight: latestBlock.GetHeight(),
		CurrentTD:     latestTD.Bytes(),
		CurrentHash:   latestBlock.GetHash(),
		GenesisHash:   genesisBlock.GetHash(),
	}
	ss.logger.Debugf("broadcast PovStatus to %d peers", peerCount)
	ss.povEngine.eb.Publish(string(common.EventBroadcast), p2p.PovStatus, status)

	now := time.Now()
	ss.allPeers.Range(func(key, value interface{}) bool {
		peer := value.(*PovSyncPeer)
		if now.Sub(peer.lastStatusTime) >= 10*time.Minute {
			if peer.status != peerStatusBad {
				ss.logger.Infof("peer %s may be dead", peer.peerID)
				peer.status = peerStatusBad
			}
		}
		return true
	})
}

func (ss *PovSyncer) checkSyncPeer() {
	latestBlock := ss.getChain().LatestBlock()

	if ss.state == common.SyncNotStart {
		ss.currentHeight = latestBlock.GetHeight()
		ss.fromHeight = ss.currentHeight + 1

		ss.setState(common.Syncing)
	} else if ss.state != common.Syncing {
		return
	}

	bestPeer := ss.BestPeer()
	if bestPeer == nil {
		if ss.syncPeerLostTime.Unix() > 0 {
			if time.Now().Unix() >= ss.syncPeerLostTime.Add(10*time.Minute).Unix() {
				ss.logger.Errorf("sync err, because no peers in 10 minutes")
				ss.setState(common.Syncerr)
			}
			return
		} else {
			ss.logger.Warnf("there is no best peer for sync, last peer %s", ss.syncPeerID)
			ss.syncPeerLostTime = time.Now()
			return
		}
	}
	ss.syncPeerLostTime = time.Time{}

	ss.syncWithPeer(bestPeer)
}

func (ss *PovSyncer) checkChain() {
	if ss.state != common.Syncing {
		return
	}

	now := time.Now()

	latestBlock := ss.getChain().LatestBlock()
	if latestBlock == nil {
		ss.logger.Infof("sync err, because current block is nil")
		ss.setState(common.Syncerr)
		return
	}

	if latestBlock.Height >= ss.toHeight {
		ss.logger.Infof("sync done, current height: %d", latestBlock.Height)
		ss.setState(common.Syncdone)
		return
	}

	ss.logger.Infof("sync current: %d, chain speed %d", latestBlock.Height, latestBlock.Height-ss.currentHeight)

	if latestBlock.Height == ss.currentHeight && now.Sub(ss.lastCheckTime) > 30*time.Minute {
		ss.logger.Infof("sync err, because progress hang, current height: %d", ss.currentHeight)
		ss.setState(common.Syncerr)
	} else if ss.state == common.Syncing {
		ss.currentHeight = latestBlock.Height
		ss.lastCheckTime = now
	}
}

func (ss *PovSyncer) setState(st common.SyncState) {
	ss.state = st
	ss.povEngine.GetEventBus().Publish(string(common.EventPovSyncState), ss.state)
}

func (ss *PovSyncer) isFinished() bool {
	if ss.state == common.SyncNotStart || ss.state == common.Syncing {
		return false
	}

	return true
}

func (ss *PovSyncer) BestPeer() *PovSyncPeer {
	var bestPeer *PovSyncPeer
	var maxTD *big.Int
	ss.allPeers.Range(func(key, value interface{}) bool {
		peer := value.(*PovSyncPeer)
		if peer.status != peerStatusGood {
			return true
		}
		if bestPeer == nil {
			maxTD = peer.currentTD
			bestPeer = peer
		} else if peer.currentTD.Cmp(maxTD) > 0 {
			maxTD = peer.currentTD
			bestPeer = peer
		}
		return true
	})

	return bestPeer
}

func (ss *PovSyncer) GetBestPeers(limit int) []*PovSyncPeer {
	var allPeers PovSyncPeerSetByTD

	ss.allPeers.Range(func(key, value interface{}) bool {
		peer := value.(*PovSyncPeer)
		if peer.status != peerStatusGood {
			return true
		}
		allPeers = append(allPeers, peer)
		return true
	})
	sort.Sort(allPeers)

	if len(allPeers) <= limit {
		return allPeers
	}

	return allPeers[:limit]
}

func (ss *PovSyncer) GetRandomPeers(limit int) []*PovSyncPeer {
	var allPeers []*PovSyncPeer
	var selectPeers []*PovSyncPeer

	ss.allPeers.Range(func(key, value interface{}) bool {
		peer := value.(*PovSyncPeer)
		if peer.status != peerStatusGood {
			return true
		}
		allPeers = append(allPeers, peer)
		return true
	})

	if len(allPeers) <= limit {
		return allPeers
	}

	rd := rand.New(rand.NewSource(time.Now().UnixNano()))
	idxSeqs := rd.Perm(len(allPeers))

	for i := 0; i < limit; i++ {
		selectPeers = append(selectPeers, allPeers[idxSeqs[i]])
	}

	return selectPeers
}

func (ss *PovSyncer) GetPeerLocators() []*PovSyncPeer {
	var allPeers PovSyncPeerSetByTD

	ss.allPeers.Range(func(key, value interface{}) bool {
		peer := value.(*PovSyncPeer)
		if peer.status != peerStatusGood {
			return true
		}
		allPeers = append(allPeers, peer)
		return true
	})
	sort.Sort(allPeers)

	if len(allPeers) <= 3 {
		return allPeers
	}

	var selectPeers []*PovSyncPeer
	selectPeers = append(selectPeers, allPeers[0])
	selectPeers = append(selectPeers, allPeers[len(allPeers)/2])
	selectPeers = append(selectPeers, allPeers[len(allPeers)-1])
	return selectPeers
}

func (ss *PovSyncer) PeerCount() int {
	peerCount := 0
	ss.allPeers.Range(func(key, value interface{}) bool {
		peerCount++
		return true
	})

	return peerCount
}

func (ss *PovSyncer) syncWithPeer(peer *PovSyncPeer) {
	if ss.syncPeerID == peer.peerID {
		if time.Now().Unix() < ss.lastSyncRequestTime.Add(15*time.Second).Unix() {
			return
		}
	}

	ss.logger.Infof("sync with peer %s to height %d", peer.peerID, peer.currentHeight)

	ss.toHeight = peer.currentHeight
	ss.syncPeerID = peer.peerID

	ss.requestSyncingBlocks(true, ss.currentHeight)
}

func (ss *PovSyncer) requestSyncingBlocks(useLocator bool, lastHeight uint64) {
	if ss.state != common.Syncing {
		return
	}

	if lastHeight >= ss.toHeight {
		return
	}

	if ss.currentHeight >= ss.toHeight {
		return
	}

	ss.lastSyncRequestTime = time.Now()

	ss.syncHeight = lastHeight + 1

	req := new(protos.PovBulkPullReq)

	req.Count = maxSyncBlockPerReq
	req.StartHeight = lastHeight
	if useLocator {
		req.Locators = ss.getChain().GetBlockLocator(types.ZeroHash)
	}
	req.Reason = protos.PovReasonSync

	ss.povEngine.eb.Publish(string(common.EventSendMsgToPeer), p2p.PovBulkPullReq, req, ss.syncPeerID)
}

func (ss *PovSyncer) requestBlocksByHeight(startHeight uint64, count uint32) {
	peer := ss.BestPeer()
	if peer == nil {
		return
	}

	req := new(protos.PovBulkPullReq)

	req.Count = count
	req.StartHeight = startHeight
	req.Reason = protos.PovReasonFetch

	ss.povEngine.eb.Publish(string(common.EventSendMsgToPeer), p2p.PovBulkPullReq, req, peer.peerID)
}

func (ss *PovSyncer) requestBlocksByHash(startHash types.Hash, count uint32, useBest bool) {
	if startHash.IsZero() || count <= 0 {
		return
	}

	var peers []*PovSyncPeer
	if useBest {
		peers := ss.GetBestPeers(2)
		if peers == nil {
			return
		}
	} else {
		peers = ss.GetPeerLocators()
		if len(peers) <= 0 {
			return
		}
	}

	req := new(protos.PovBulkPullReq)

	req.Count = count
	req.StartHash = startHash
	req.Reason = protos.PovReasonFetch

	for _, peer := range peers {
		ss.logger.Debugf("request block start %s count %d from peer %s", startHash, count, peer.peerID)
		ss.povEngine.eb.Publish(string(common.EventSendMsgToPeer), p2p.PovBulkPullReq, req, peer.peerID)
	}
}

func (ss *PovSyncer) requestTxsByHash(startHash types.Hash, endHash types.Hash) {
	peers := ss.GetBestPeers(3)
	if len(peers) <= 0 {
		return
	}

	req := new(protos.BulkPullReqPacket)

	req.StartHash = startHash
	req.EndHash = endHash

	for _, peer := range peers {
		ss.logger.Debugf("request tx start %s end %s from peer %s", startHash, endHash, peer.peerID)
		ss.povEngine.eb.Publish(string(common.EventSendMsgToPeer), p2p.BulkPullRequest, req, peer.peerID)
	}
}
