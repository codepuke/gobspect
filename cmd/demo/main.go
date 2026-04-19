package main

import (
	"encoding/gob"
	"log"
	"math/rand"
	"os"
	"time"
)

type LineItem struct {
	Price    float64
	Quantity int
	SKU      string
}

type Order struct {
	Customer string
	ID       uint
	Items    []LineItem
	PlacedAt time.Time
	Status   string
	Tags     map[string]string
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: demo <output.gob>")
	}
	f, err := os.Create(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	enc := gob.NewEncoder(f)

	customers := []string{"Alice", "Bob", "Charlie", "Diana", "Eve", "Frank"}
	skus := []string{"A1", "A2", "B1", "B2", "C1", "C2", "D1", "D2", "X9", "Y12"}
	statuses := []string{"pending", "shipped", "delivered", "cancelled", "refunded"}

	r := rand.New(rand.NewSource(42))

	for i := range 1500 {
		numItems := r.Intn(5) + 1
		items := make([]LineItem, numItems)
		for j := range numItems {
			items[j] = LineItem{
				SKU:      skus[r.Intn(len(skus))],
				Quantity: r.Intn(10) + 1,
				Price:    float64(r.Intn(10000)) / 100.0,
			}
		}

		order := Order{
			Customer: customers[r.Intn(len(customers))],
			ID:       uint(i + 1000),
			Items:    items,
			PlacedAt: time.Date(2024, time.March, r.Intn(30)+1, r.Intn(24), r.Intn(60), 0, 0, time.UTC),
			Status:   statuses[r.Intn(len(statuses))],
			Tags:     map[string]string{"region": "US", "source": "web"},
		}

		if err := enc.Encode(order); err != nil {
			log.Fatal(err)
		}
	}

	log.Printf("Successfully wrote 1500 orders to %s", os.Args[1])
}
