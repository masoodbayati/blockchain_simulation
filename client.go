package main

import (
	"bytes"
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"time"
)

func main() {
	for i := 0; i <= 10000; i++ {
		bpm := rand.Intn(100)
		values := map[string]int{"BPM": bpm}
		json_data, err := json.Marshal(values)

		if err != nil {
			log.Fatal(err)
		}
		go func() {
			_, _ = http.Post("http://localhost:9091", "application/json",
				bytes.NewBuffer(json_data))
			//var res map[string]interface{}
			//json.NewDecoder(resp.Body).Decode(&res)
			//fmt.Println(res["json"])
		}()

		time.Sleep(time.Millisecond * 200)
	}

}
