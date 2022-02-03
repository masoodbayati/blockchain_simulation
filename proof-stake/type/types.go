package type_def

import "encoding/json"

type Block struct {
	Typ string	`json:"typ"`
	OwnerNick  string `json:"owner_nick"`
	Index      int	`json:"index"`
	Timestamp  string `json:"timestamp"`
	BPM        int	`json:"bpm"`
	Hash       string `json:"hash"`
	PrevHash   string	`json:"prev_hash"`
	Nonce      string	`json:"nonce"`
	Validator  string `json:"validator"`
}
type BlockMessage struct {
	BPM int `json:"bpm"`
	Typ string `json:"typ"`
	Index int `json:"index"`
}

func Unmarshl(data []byte) (interface{},error) {
	var typ struct {
		Type string `json:"typ"`
	}
	if err := json.Unmarshal(data, &typ); err != nil {
		return 0,err
	}
	switch typ.Type {
	case "generated_block":
		generated_block := new(Block)
		err := json.Unmarshal(data,generated_block)
		if err != nil {
			println("json_unmarshl_Err")
			return 0,err
		}
		return generated_block,nil
	case "pending_block":
		pending_block := new(BlockMessage)
		err := json.Unmarshal(data,pending_block)
		if err != nil {
			println("json_unmarshl_Err")
			return 0,err
		}
		return pending_block,nil
	default:
		return 0,nil
	}
	return 0,nil
}

