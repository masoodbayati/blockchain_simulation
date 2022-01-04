package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/blockchain-tutorial/proof-work/chatroom"
	type_def "github.com/blockchain-tutorial/proof-work/type"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
)

const difficulty = 1

var nick string
var cr *chatroom.ChatRoom

// Block represents each 'item' in the blockchain

// Blockchain is a series of validated Blocks
var pendingBlock chan *type_def.Block
var Blockchain []type_def.Block

// Message takes incoming JSON payload for writing heart rate


var mutex = &sync.Mutex{}

func main() {
	pendingBlock = make(chan *type_def.Block, chatroom.ChatRoomBufSize)
	err := godotenv.Load()
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		t := time.Now()
		genesisBlock := type_def.Block{}
		genesisBlock = type_def.Block{"generated_block","",0, t.String(), 0, calculateHash(genesisBlock), "", difficulty, ""}
		spew.Dump(genesisBlock)

		mutex.Lock()
		Blockchain = append(Blockchain, genesisBlock)
		mutex.Unlock()
	}()
	log.Fatal(run())

}



// web server
func run() error {
	ctx := context.Background()
	roomFlag := "test"

	// create a new libp2p Host that listens on a random TCP port
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"))
	if err != nil {
		panic(err)
	}

	// create a new PubSub service using the GossipSub router
	ps, err := pubsub.NewGossipSub(ctx, h)
	if err != nil {
		panic(err)
	}

	// setup local mDNS discovery
	if err := setupDiscovery(h); err != nil {
		panic(err)
	}

	// use the nickname from the cli flag, or a default if blank


	nick = defaultNick(h.ID())

	// join the room from the cli flag, or the flag default

	// join the chat room
	cr, err = chatroom.JoinChatRoom(ctx, ps, h.ID(), nick, roomFlag)
	if err != nil {
		panic(err)
	}
	mux := makeMuxRouter()
	httpPort := os.Getenv("PORT")
	log.Println("HTTP Server Listening on port :", httpPort)
	s := &http.Server{
		Addr:           ":" + httpPort,
		Handler:        mux,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	go receveMessage()
	if err := s.ListenAndServe(); err != nil {
		return err
	}

	return nil
}

func receveMessage() {
	for {
		message :=<- cr.Messages
		println(message.Message,message.SenderID)
		map_message := message.Message.(map[string]interface{})
		switch map_message["typ"] {
		case "pending_block":
			mutex.Lock()
			newBlock := generateBlock(Blockchain[len(Blockchain)-1], int(map_message["bpm"].(float64)))
			mutex.Unlock()

			if isBlockValid(newBlock, Blockchain[len(Blockchain)-1]) {
				Blockchain = append(Blockchain, newBlock)
				spew.Dump(Blockchain)
			}
			if newBlock.OwnerNick == nick{
				err := cr.Publish(newBlock)
				if err!= nil{
					println("err in publish",err)
				}
			}

		case "generated_block":
			generated_block := type_def.Block{
				Typ:        map_message["typ"].(string),
				OwnerNick:  map_message["owner_nick"].(string),
				Index:      int(map_message["index"].(float64)),
				Timestamp:  map_message["timestamp"].(string),
				BPM:        int(map_message["bpm"].(float64)),
				Hash:       map_message["hash"].(string),
				PrevHash:   map_message["prev_hash"].(string),
				Difficulty: int(map_message["difficulty"].(float64)),
				Nonce:      map_message["nonce"].(string),
			}
			pendingBlock <- &generated_block
		default:
			println("receive bad message")
			continue
		}


		}
}



// create handlers
func makeMuxRouter() http.Handler {
	muxRouter := mux.NewRouter()
	muxRouter.HandleFunc("/", handleGetBlockchain).Methods("GET")
	muxRouter.HandleFunc("/", handleWriteBlock).Methods("POST")
	return muxRouter
}

// write blockchain when we receive an http request
func handleGetBlockchain(w http.ResponseWriter, r *http.Request) {
	bytes, err := json.MarshalIndent(Blockchain, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	io.WriteString(w, string(bytes))
}

// takes JSON payload as an input for heart rate (BPM)
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

	//ensure atomicity when creating new block
	err := cr.Publish(m)
	if err!= nil{
		println("err in publish",err)
	}
	mutex.Lock()
	newBlock := generateBlock(Blockchain[len(Blockchain)-1], m.BPM)
	mutex.Unlock()

	if isBlockValid(newBlock, Blockchain[len(Blockchain)-1]) {
		Blockchain = append(Blockchain, newBlock)
		spew.Dump(Blockchain)
	}
	if newBlock.OwnerNick == nick{
		err := cr.Publish(newBlock)
		if err!= nil{
			println("err in publish",err)
		}
	}

	respondWithJSON(w, r, http.StatusCreated, newBlock)

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

// make sure block is valid by checking index, and comparing the hash of the previous block
func isBlockValid(newBlock, oldBlock type_def.Block) bool {
	if oldBlock.Index+1 != newBlock.Index {
		return false
	}

	if oldBlock.Hash != newBlock.PrevHash {
		return false
	}

	if calculateHash(newBlock) != newBlock.Hash {
		return false
	}

	return true
}

// SHA256 hasing
func calculateHash(block type_def.Block) string {
	record := strconv.Itoa(block.Index) + block.Timestamp + strconv.Itoa(block.BPM) + block.PrevHash + block.Nonce
	h := sha256.New()
	h.Write([]byte(record))
	hashed := h.Sum(nil)
	return hex.EncodeToString(hashed)
}

// create a new block using previous block's hash
func generateBlock(oldBlock type_def.Block, BPM int) type_def.Block {
	var newBlock type_def.Block

	t := time.Now()

	newBlock.Index = oldBlock.Index + 1
	newBlock.Timestamp = t.String()
	newBlock.BPM = BPM
	newBlock.PrevHash = oldBlock.Hash
	newBlock.Difficulty = difficulty
	newBlock.OwnerNick = nick
	newBlock.Typ = "generated_block"

	for i := 0; ; i++ {
		hex := fmt.Sprintf("%x", i)
		newBlock.Nonce = hex
		if len(pendingBlock)>0{
			pendBlock :=<- pendingBlock
			if pendBlock.PrevHash == newBlock.PrevHash{
				println("we loose")
				newBlock = *pendBlock
				return newBlock
			}
		}
		if !isHashValid(calculateHash(newBlock), newBlock.Difficulty) {
			fmt.Println(calculateHash(newBlock), " do more work!")
			time.Sleep(time.Second)
			continue
		} else {
			fmt.Println(calculateHash(newBlock), " work done!")
			newBlock.Hash = calculateHash(newBlock)
			break
		}
	}
	if len(pendingBlock)>0{
		pendBlock :=<- pendingBlock
		if pendBlock.PrevHash == newBlock.PrevHash{
			println("we loose")
			newBlock = *pendBlock
			return newBlock
		}
	}

	return newBlock
}

func isHashValid(hash string, difficulty int) bool {
	prefix := strings.Repeat("0", difficulty)
	return strings.HasPrefix(hash, prefix)
}


type discoveryNotifee struct {
	h host.Host
}
const DiscoveryServiceTag = "pubsub-chat-example"
func (n *discoveryNotifee) HandlePeerFound(pi peer.AddrInfo) {
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