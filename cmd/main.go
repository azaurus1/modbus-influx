package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api/write"
	"github.com/simonvetter/modbus"
)

const name = "github.com/azaurus1/modbus-influx"

var (
	mu        sync.Mutex
	registers map[int]int64
)

func init() {
	registers = make(map[int]int64)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// set up influx db connection
	token := os.Getenv("INFLUXDB_TOKEN")
	url := "http://localhost:8086"
	client := influxdb2.NewClient(url, token)

	// Setup the Modbus client
	modbusClient, err := modbus.NewClient(&modbus.ClientConfiguration{
		URL:     "tcp://localhost:5020",
		Timeout: 1 * time.Second,
	})
	if err != nil {
		log.Fatalf("Could not create client: %v", err)
		return
	}
	defer modbusClient.Close()

	if err := modbusClient.Open(); err != nil {
		log.Fatalf("Could not open Modbus connection: %v", err)
		return
	}

	var wg sync.WaitGroup

	// Start a goroutine for each register to update it independently
	for i := 1; i <= 100; i++ {
		wg.Add(1)
		go updateRegister(ctx, modbusClient, client, i, &wg)
	}

	// Wait for interrupt signal
	<-ctx.Done()
	log.Println("Received interrupt signal, shutting down...")

	// Wait for all goroutines to finish
	wg.Wait()
}

func recordRegisterValue(client influxdb2.Client, register int, value int64) {
	mu.Lock() // Ensure safe access to the map
	defer mu.Unlock()

	org := "azaurus"
	bucket := "modbus"
	writeAPI := client.WriteAPI(org, bucket)
	tags := map[string]string{
		"holding_register": strconv.Itoa(register),
	}
	fields := map[string]interface{}{
		"value": value,
	}
	name := fmt.Sprintf("holding_register_%d", register)
	point := write.NewPoint(name, tags, fields, time.Now())

	writeAPI.WritePoint(point)

}

func updateRegister(ctx context.Context, client *modbus.ModbusClient, influxClient influxdb2.Client, register int, wg *sync.WaitGroup) {
	defer wg.Done()
	ticker := time.NewTicker(1 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Read from Modbus register
			reg16, err := client.ReadRegister(uint16(register), modbus.HOLDING_REGISTER)
			if err != nil {
				log.Printf("Error reading register %d: %v", register, err)
				continue
			}
			// Record the register value
			recordRegisterValue(influxClient, register, int64(reg16))
		case <-ctx.Done():
			log.Printf("Stopping updates for register %d", register)
			return
		}
	}
}
