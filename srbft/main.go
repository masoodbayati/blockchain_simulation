package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/blockchain-tutorial/srbft/chatroom"
	type_def "github.com/blockchain-tutorial/srbft/type"
	"github.com/gorilla/mux"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"log"
	"math"
	"math/big"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/davecgh/go-spew/spew"
)

// Block represents each 'item' in the blockchain

// Blockchain is a series of validated Blocks
var Blockchain []type_def.Block
var tempBlocks []type_def.BlockMessage
var balance int

// candidateBlocks handles incoming blocks for validation
var candidateBlocks = make(chan type_def.BlockMessage)
var submitChannel = make(chan type_def.Block)

// announcements broadcasts winning validator to all nodes

var mutex = &sync.Mutex{}

// validators keeps track of open validators and balances
var validators = make(map[string]int)
var nick string
var cr *chatroom.ChatRoom
var validatorChannel *chatroom.ChatRoom
var address host.Host
var isCandidate *bool

func main() {
	ctx := context.Background()
	roomFlag := flag.String("network", "0", "network_id")
	httpPort := flag.String("port", "8080", "an int")
	isCandidate = flag.Bool("is_candidate", false, "is candidate")
	flag.Parse()
	// create a new libp2p Host that listens on a random TCP port
	address, _ = libp2p.New(libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"))

	// create a new PubSub service using the GossipSub router
	ps, err := pubsub.NewGossipSub(ctx, address)
	if err != nil {
		panic(err)
	}

	// setup local mDNS discovery
	if err := setupDiscovery(address); err != nil {
		panic(err)
	}

	// use the nickname from the cli flag, or a default if blank

	nick = defaultNick(address.ID())

	// join the room from the cli flag, or the flag default

	// join the chat room
	cr, err = chatroom.JoinChatRoom(ctx, ps, address.ID(), nick, *roomFlag)
	time.Sleep(time.Second)
	balance = sendBalnce(address.ID())
	mutex.Lock()
	validators[address.ID().String()] = balance
	mutex.Unlock()

	if err != nil {
		panic(err)
	}
	mux := makeMuxRouter()

	//httpPort := "8082"
	log.Println("HTTP Server Listening on port :", *httpPort)
	s := &http.Server{
		Addr:           ":" + *httpPort,
		Handler:        mux,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	// create genesis block
	t := time.Now()
	genesisBlock := type_def.Block{}
	genesisBlock = type_def.Block{
		Typ:       "generated_block",
		OwnerNick: "",
		Index:     0,
		Timestamp: t.String(),
		BPM:       0,
		Hash:      calculateBlockHash(genesisBlock),
		PrevHash:  "",
		Nonce:     "",
		Validator: "",
	}
	spew.Dump(genesisBlock)
	Blockchain = append(Blockchain, genesisBlock)

	go func() {
		for candidate := range candidateBlocks {
			mutex.Lock()
			tempBlocks = append(tempBlocks, candidate)
			mutex.Unlock()
		}
	}()
	if *isCandidate {
		validatorChannel, err = chatroom.JoinChatRoom(ctx, ps, address.ID(), nick, "validators")
		go pickWinner()
	}
	go receveMessage()
	if err := s.ListenAndServe(); err != nil {
		return
	}
}

func sendBalnce(id peer.ID) int {
	time.Sleep(time.Second)
	e_data, _ := base64.RawURLEncoding.DecodeString(id.String())
	newPeerId := int(math.Abs(float64(new(big.Int).SetBytes(e_data).Int64()))) % 100
	if newPeerId == 0 {
		newPeerId = 1
	}
	cr.Publish(type_def.Balance{
		Value:       newPeerId,
		Typ:         "balance",
		IsCandidate: *isCandidate,
	})
	return newPeerId
}

func receveMessage() {
	for {
		message := <-cr.Messages
		println(message.Message, message.SenderID)
		map_message := message.Message.(map[string]interface{})
		switch map_message["typ"] {
		case "pending_block":
			candidateBlocks <- type_def.BlockMessage{
				BPM:   int(map_message["bpm"].(float64)),
				Index: int(map_message["index"].(float64)),
				Typ:   "pending_block",
			}

		case "generated_block":
			generated_block := type_def.Block{
				Typ:       map_message["typ"].(string),
				OwnerNick: map_message["owner_nick"].(string),
				Index:     int(map_message["index"].(float64)),
				Timestamp: map_message["timestamp"].(string),
				BPM:       int(map_message["bpm"].(float64)),
				Hash:      map_message["hash"].(string),
				PrevHash:  map_message["prev_hash"].(string),
				Nonce:     map_message["nonce"].(string),
			}
			mutex.Lock()
			oldLastBlock := Blockchain[len(Blockchain)-1]
			mutex.Unlock()
			newBlock := generated_block
			if CheckBlockValid(newBlock, oldLastBlock) {
				println("newBlock", newBlock.Index, newBlock.BPM)
				mutex.Lock()
				Blockchain = append(Blockchain, newBlock)
				mutex.Unlock()
				println("new block added")
			} else {
				println("block is invalid")
			}
		case "delegate":
			owner := map_message["from_address"].(string)
			delegate_balance := int(map_message["balance"].(float64))
			delegateTo := map_message["to_address"].(string)
			mutex.Lock()
			owner_balance := validators[owner]
			validators[owner] = owner_balance - delegate_balance
			validators[delegateTo] = validators[delegateTo] + delegate_balance
			if delegateTo == address.ID().String() {
				balance = validators[delegateTo] + delegate_balance
			}
			println("delegate balance from ", owner, delegate_balance, delegateTo)
			mutex.Unlock()
		case "balance":
			mutex.Lock()
			validators[message.SenderID] = int(map_message["value"].(float64))
			mutex.Unlock()
			if map_message["is_candidate"].(bool) {
				delegateBalance := balance - 1
				balance = 1
				cr.Publish(type_def.Delegate{
					Balance:     delegateBalance,
					FromAddress: address.ID().String(),
					ToAddress:   message.SenderID,
					Typ:         "delegate",
				})
				validators[address.ID().String()] = 1
				validators[message.SenderID] = validators[message.SenderID] + delegateBalance
				println("delegate balance from ", address.ID().String(), delegateBalance, message.SenderID)
			}
		case "submit":
			if !*isCandidate {
				continue
			}
			submited_block := type_def.Block{
				Typ:       map_message["typ"].(string),
				OwnerNick: map_message["owner_nick"].(string),
				Index:     int(map_message["index"].(float64)),
				Timestamp: map_message["timestamp"].(string),
				BPM:       int(map_message["bpm"].(float64)),
				Hash:      map_message["hash"].(string),
				PrevHash:  map_message["prev_hash"].(string),
				Nonce:     map_message["nonce"].(string),
			}
			mutex.Lock()
			oldLastBlock := Blockchain[len(Blockchain)-1]
			mutex.Unlock()
			newBlock := submited_block
			if CheckBlockValid(newBlock, oldLastBlock) {
				submited_block.Typ = "ack_submit"
				validatorChannel.Publish(submited_block)
			}
		case "ack_submit":
			ack_submited_block := type_def.Block{
				Typ:       map_message["typ"].(string),
				OwnerNick: map_message["owner_nick"].(string),
				Index:     int(map_message["index"].(float64)),
				Timestamp: map_message["timestamp"].(string),
				BPM:       int(map_message["bpm"].(float64)),
				Hash:      map_message["hash"].(string),
				PrevHash:  map_message["prev_hash"].(string),
				Nonce:     map_message["nonce"].(string),
			}
			if *isCandidate {
				submitChannel <- ack_submited_block
			}

		default:
			println("receive bad message")
			continue
		}

	}
}

// pickWinner creates a lottery pool of validators and chooses the validator who gets to forge a block to the blockchain
// by random selecting from the pool, weighted by amount of tokens staked
func pickWinner() {
	time.Sleep(10 * time.Second)
	for {

		//rounTime := time.Now().Unix()/10 % 1000000

		mutex.Lock()
		temp := tempBlocks
		mutex.Unlock()

		lotteryPool := []string{}
		if len(temp) > 0 {

			// slightly modified traditional proof of stake algorithm
			// from all validators who submitted a block, weight them by the number of staked tokens
			// in traditional proof of stake, validators can participate without submitting a block to be forged
			for validator, balance := range validators {
				for i := 0; i < balance; i++ {
					lotteryPool = append(lotteryPool, validator)
				}
			}
			if balance <= len(lotteryPool)/2 {
				time.Sleep(time.Second)
				continue
			}

			//mutex.Lock()
			//lastBlock := Blockchain[len(Blockchain)-1]
			//mutex.Unlock()
			//winnerIndex := lastBlock.BPM%len(lotteryPool)
			//winner := lotteryPool[winnerIndex]
			//println("winner",winnerIndex,winner)
			//if winner != address.ID().String() {
			//	println("not win")
			//	time.Sleep(time.Second)
			//	continue
			//}

			// add block of winner to blockchain and let all the other nodes know

			for _, block := range temp {
				mutex.Lock()
				temp = temp[1:]
				tempBlocks = tempBlocks[1:]
				lastBlock := Blockchain[len(Blockchain)-1]
				mutex.Unlock()
				if block.Index <= lastBlock.Index {
					continue
				}

				newBlock, _ := generateBlock(lastBlock, block.BPM, address.ID().String())
				if isBlockValid(newBlock, lastBlock) {
					println("newBlock", newBlock.Index, newBlock.BPM)
					mutex.Lock()
					Blockchain = append(Blockchain, newBlock)
					mutex.Unlock()
					newBlock.Typ = "generated_block"
					validatorChannel.Publish(newBlock)
					time.Sleep(time.Second)
					println("new block added")
					break
				} else {
					println("block is invalid")
				}

			}
		}
	}
}

// isBlockValid makes sure block is valid by checking index
// and comparing the hash of the previous block
func isBlockValid(newBlock, oldBlock type_def.Block) bool {
	blockValidatorLen := 0
	if oldBlock.Index >= newBlock.Index {
		return false
	}

	if oldBlock.Hash != newBlock.PrevHash {
		return false
	}

	if calculateBlockHash(newBlock) != newBlock.Hash {
		return false
	}
	newBlock.Typ = "submit"
	cr.Publish(newBlock)
	timeout := make(chan bool, 1)
	go func() {
		time.Sleep(5 * time.Second)
		timeout <- true
	}()
	select {
	case ch := <-submitChannel:
		if ch.Hash == newBlock.Hash {
			blockValidatorLen++
		}
		if blockValidatorLen > 1 {
			println("block validated")
			return true
		}
		// a read from ch has occurred
	case <-timeout:
		return false
		// the read from ch has timed out
	}
	return true
}
func CheckBlockValid(newBlock, oldBlock type_def.Block) bool {
	if oldBlock.Index >= newBlock.Index {
		return false
	}

	if oldBlock.Hash != newBlock.PrevHash {
		return false
	}

	if calculateBlockHash(newBlock) != newBlock.Hash {
		return false
	}

	return true
}

// SHA256 hasing
// calculateHash is a simple SHA256 hashing function
func calculateHash(s string) string {
	h := sha256.New()
	h.Write([]byte(s))
	hashed := h.Sum(nil)
	return hex.EncodeToString(hashed)
}

//calculateBlockHash returns the hash of all block information
func calculateBlockHash(block type_def.Block) string {
	record := string(block.Index) + block.Timestamp + string(block.BPM) + block.PrevHash
	return calculateHash(record)
}

// generateBlock creates a new block using previous block's hash
func generateBlock(oldBlock type_def.Block, BPM int, address string) (type_def.Block, error) {

	var newBlock type_def.Block

	t := time.Now()

	newBlock.Index = oldBlock.Index + 1
	newBlock.Timestamp = t.String()
	newBlock.BPM = BPM
	newBlock.PrevHash = oldBlock.Hash
	newBlock.Hash = calculateBlockHash(newBlock)
	newBlock.Validator = address
	newBlock.Typ = "generated_block"

	return newBlock, nil
}

func makeMuxRouter() http.Handler {
	muxRouter := mux.NewRouter()
	muxRouter.HandleFunc("/", handleWriteBlock).Methods("POST")
	return muxRouter
}

func handleWriteBlock(w http.ResponseWriter, r *http.Request) {

	w.Header().Set("Content-Type", "application/json")
	var m type_def.BlockMessage

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&m); err != nil {
		respondWithJSON(w, r, http.StatusBadRequest, r.Body)
		return
	}
	defer r.Body.Close()
	m.Typ = "pending_block"

	mutex.Lock()
	lastBlock := Blockchain[len(Blockchain)-1]
	oldLastIndex := lastBlock.Index

	if len(tempBlocks) > 0 && tempBlocks[len(tempBlocks)-1].Index > lastBlock.Index {
		oldLastIndex = tempBlocks[len(tempBlocks)-1].Index
	}
	mutex.Unlock()
	m.Index = oldLastIndex + 1

	// create newBlock for consideration to be forged
	candidateBlocks <- type_def.BlockMessage{
		BPM:   m.BPM,
		Typ:   m.Typ,
		Index: m.Index,
	}
	println()
	err := cr.Publish(m)
	if err != nil {
		println("err in publish", err)
	}
	respondWithJSON(w, r, http.StatusCreated, "test")
}

func respondWithJSON(w http.ResponseWriter, r *http.Request, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	response, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("HTTP 500: Internal Server Error"))
		return
	}
	w.WriteHeader(code)
	w.Write(response)
}

type discoveryNotifee struct {
	h host.Host
}

const DiscoveryServiceTag = "pubsub-chat-example"

func (n *discoveryNotifee) HandlePeerFound(pi peer.AddrInfo) {
	//e_data, _ := base64.RawURLEncoding.DecodeString(pi.ID.String())
	//newPeerId := int(math.Abs(float64(new(big.Int).SetBytes(e_data).Int64())))%100
	//if pi.ID.String()== address.ID().String(){
	//	balance = newPeerId
	//}
	//println("new peer id",newPeerId)
	//if newPeerId == 0{
	//	newPeerId = 1
	//}
	//mutex.Lock()
	//validators[pi.ID.String()] = newPeerId
	//mutex.Unlock()
	//if balance >50 && pi.ID.String() != address.ID().String(){
	//	delegateBalance := balance-50
	//	balance = 50
	//	cr.Publish(type_def.Delegate{
	//		Balance: delegateBalance,
	//		FromAddress: address.ID().String(),
	//		ToAddress: pi.ID.String(),
	//		Typ:     "delegate",
	//	})
	//	mutex.Lock()
	//	validators[address.ID().String()] = 50
	//	validators[pi.ID.String()] = newPeerId+delegateBalance
	//	println("delegate balance from ",address.ID().String(),delegateBalance,pi.ID.String())
	//	mutex.Unlock()
	//}

	fmt.Printf("discovered new peer %s\n", pi.ID.Pretty())
	err := n.h.Connect(context.Background(), pi)
	if err != nil {
		fmt.Printf("error connecting to peer %s: %s\n", pi.ID.Pretty(), err)
	}
}
func setupDiscovery(h host.Host) error {
	// setup mDNS discovery to find local peers
	s := mdns.NewMdnsService(h, DiscoveryServiceTag, &discoveryNotifee{h: h})
	return s.Start()
}
func defaultNick(p peer.ID) string {
	return fmt.Sprintf("%s-%s", os.Getenv("USER"), shortID(p))
}
func shortID(p peer.ID) string {
	pretty := p.Pretty()
	return pretty[len(pretty)-8:]
}
