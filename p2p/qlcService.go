package p2p

import (
	"time"

	"github.com/AsynkronIT/protoactor-go/actor"

	"github.com/qlcchain/go-qlc/common/topic"

	"github.com/qlcchain/go-qlc/chain/context"
	chainctx "github.com/qlcchain/go-qlc/chain/context"
	"github.com/qlcchain/go-qlc/common/event"
	"github.com/qlcchain/go-qlc/ledger"
)

// QlcService service for qlc p2p network
type QlcService struct {
	subscriber *event.ActorSubscriber
	node       *QlcNode
	dispatcher *Dispatcher
	msgEvent   event.EventBus
	msgService *MessageService
	cc         *chainctx.ChainContext
}

// NewQlcService create netService
func NewQlcService(cfgFile string) (*QlcService, error) {
	cc := context.NewChainContext(cfgFile)
	cfg, _ := cc.Config()
	node, err := NewNode(cfg)
	if err != nil {
		return nil, err
	}
	ns := &QlcService{
		node:       node,
		dispatcher: NewDispatcher(),
		msgEvent:   cc.EventBus(),
		cc:         cc,
	}
	node.SetQlcService(ns)
	l := ledger.NewLedger(cfgFile)
	msgService := NewMessageService(ns, l)
	ns.msgService = msgService
	return ns, nil
}

// Node return the peer node
func (ns *QlcService) Node() *QlcNode {
	return ns.node
}

// EventQueue return EventQueue
func (ns *QlcService) MessageEvent() event.EventBus {
	return ns.msgEvent
}

// Start start p2p manager.
func (ns *QlcService) Start() error {
	//ns.node.logger.Info("Starting QlcService...")

	// start dispatcher.
	ns.dispatcher.Start()

	//set event
	err := ns.setEvent()
	if err != nil {
		return err
	}
	// start node.
	if err := ns.node.StartServices(); err != nil {
		ns.dispatcher.Stop()
		ns.node.logger.Error("Failed to start QlcService.")
		return err
	}
	// start msgService
	ns.msgService.Start()
	ns.node.logger.Info("Started QlcService.")
	return nil
}

func (ns *QlcService) setEvent() error {
//<<<<<<< HEAD
//	id, err := ns.msgEvent.Subscribe(common.EventBroadcast, ns.Broadcast)
//	if err != nil {
//		ns.node.logger.Error(err)
//		return err
//	}
//	ns.handlerIds[common.EventBroadcast] = id
//	id, err = ns.msgEvent.Subscribe(common.EventSendMsgToSingle, ns.SendMessageToPeer)
//	if err != nil {
//		ns.node.logger.Error(err)
//		return err
//	}
//	ns.handlerIds[common.EventSendMsgToSingle] = id
//	id, err = ns.msgEvent.SubscribeSync(common.EventPeersInfo, ns.node.streamManager.GetAllConnectPeersInfo)
//	if err != nil {
//		ns.node.logger.Error(err)
//		return err
//	}
//	ns.handlerIds[common.EventPeersInfo] = id
//	id, err = ns.msgEvent.SubscribeSync(common.EventOnlinePeersInfo, ns.node.streamManager.GetOnlinePeersInfo)
//	if err != nil {
//		ns.node.logger.Error(err)
//		return err
//	}
//	ns.handlerIds[common.EventOnlinePeersInfo] = id
//	id, err = ns.msgEvent.Subscribe(common.EventFrontiersReq, ns.msgService.syncService.requestFrontiersFromPov)
//	if err != nil {
//		ns.node.logger.Error(err)
//		return err
//	}
//	ns.handlerIds[common.EventFrontiersReq] = id
//	id, err = ns.msgEvent.Subscribe(common.EventRepresentativeNode, ns.node.setRepresentativeNode)
//	if err != nil {
//		ns.node.logger.Error(err)
//		return err
//	}
//	ns.handlerIds[common.EventRepresentativeNode] = id
//	id, err = ns.msgEvent.SubscribeSync(common.EventGetBandwidthStats, ns.node.GetBandwidthStats)
//	if err != nil {
//		ns.node.logger.Error(err)
//		return err
//	}
//	ns.handlerIds[common.EventGetBandwidthStats] = id
//	id, err = ns.msgEvent.SubscribeSync(common.EventConsensusSyncFinished, ns.msgService.syncService.onConsensusSyncFinished)
//	if err != nil {
//		ns.node.logger.Error(err)
//		return err
//	}
//	ns.handlerIds[common.EventConsensusSyncFinished] = id
//	id, err = ns.msgEvent.SubscribeSync(common.EventSyncStatus, ns.msgService.syncService.GetSyncState)
//	if err != nil {
//=======
	ns.subscriber = event.NewActorSubscriber(event.SpawnWithPool(func(c actor.Context) {
		switch msg := c.Message().(type) {
		case *EventBroadcastMsg:
			ns.Broadcast(msg.Type, msg.Message)
		case *EventSendMsgToSingleMsg:
			if err := ns.SendMessageToPeer(msg.Type, msg.Message, msg.PeerID); err != nil {
				ns.node.logger.Error(err)
			}
		case *EventFrontiersReqMsg:
			ns.msgService.syncService.requestFrontiersFromPov(msg.PeerID)
		case bool:
			ns.node.setRepresentativeNode(msg)
		case *topic.EventP2PSyncStateMsg:
			ns.msgService.syncService.onConsensusSyncFinished()
		}
	}), ns.msgEvent)

	if err := ns.subscriber.Subscribe(topic.EventBroadcast, topic.EventSendMsgToSingle, topic.EventFrontiersReq,
		topic.EventRepresentativeNode, topic.EventConsensusSyncFinished); err != nil {
		ns.node.logger.Error(err)
		return err
	}

	return nil
}

// Stop stop p2p manager.
func (ns *QlcService) Stop() error {
	//ns.node.logger.Info("Stopping QlcService...")

	//this must be the first step
	err := ns.subscriber.UnsubscribeAll()
	if err != nil {
		return err
	}

	if err := ns.node.Stop(); err != nil {
		return err
	}

	ns.dispatcher.Stop()
	ns.msgService.Stop()

	time.Sleep(100 * time.Millisecond)
	return nil
}

// Register register the subscribers.
func (ns *QlcService) Register(subscribers ...*Subscriber) {
	ns.dispatcher.Register(subscribers...)
}

// Deregister Deregister the subscribers.
func (ns *QlcService) Deregister(subscribers *Subscriber) {
	ns.dispatcher.Deregister(subscribers)
}

// PutMessage put snyc message to dispatcher.
func (ns *QlcService) PutSyncMessage(msg *Message) {
	ns.dispatcher.PutSyncMessage(msg)
}

// PutMessage put dpos message to dispatcher.
func (ns *QlcService) PutMessage(msg *Message) {
	ns.dispatcher.PutMessage(msg)
}

// Broadcast message.
func (ns *QlcService) Broadcast(name MessageType, value interface{}) {
	ns.node.BroadcastMessage(name, value)
}

//func (ns *QlcService) SendMessageToPeers(messageName MessageType, value interface{}, peerID string) {
//	ns.node.SendMessageToPeers(messageName, value, peerID)
//}

// SendMessageToPeer send message to a peer.
func (ns *QlcService) SendMessageToPeer(messageName MessageType, value interface{}, peerID string) error {
	return ns.node.SendMessageToPeer(messageName, value, peerID)
}
