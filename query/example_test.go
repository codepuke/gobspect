package query_test

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"log"

	"github.com/codepuke/gobspect"
	"github.com/codepuke/gobspect/query"
)

// Example_queryGet decodes a real gob stream and navigates the resulting
// Value tree with dot paths, a wildcard, and a negative index.
func Example_queryGet() {
	// snippet:start query-get
	type Customer struct {
		Name string
	}
	type Order struct {
		ID       int
		Total    float64
		Customer Customer
	}
	type Report struct {
		Orders []Order
	}

	// Encode a gob stream the usual way.
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(Report{Orders: []Order{
		{ID: 1, Total: 9.5, Customer: Customer{Name: "Ada"}},
		{ID: 2, Total: 24, Customer: Customer{Name: "Grace"}},
		{ID: 3, Total: 12.25, Customer: Customer{Name: "Linus"}},
	}}); err != nil {
		log.Fatal(err)
	}

	// Decode it with gobspect — no original types needed.
	values, err := gobspect.New().Stream(&buf).Collect()
	if err != nil {
		log.Fatal(err)
	}
	root := values[0]

	// Dot paths navigate struct fields and slice indexes.
	name, _ := query.Get(root, "Orders.0.Customer.Name")
	fmt.Println(gobspect.Format(name))

	// Negative indexes count from the end: -1 is the last element.
	last, _ := query.Get(root, "Orders.-1.Customer.Name")
	fmt.Println(gobspect.Format(last))

	// A wildcard fans out across every element.
	for _, v := range query.All(root, "Orders.*.Total") {
		fmt.Println(gobspect.Format(v))
	}
	// snippet:end

	// Output:
	// "Ada"
	// "Linus"
	// 9.5
	// 24.0
	// 12.25
}

// Example_queryDescent finds nodes at any depth with recursive descent,
// narrows them with a filter, and projects a subset of their fields.
func Example_queryDescent() {
	// snippet:start query-descent
	type Server struct {
		Host   string
		Status string
		Tags   []string
	}
	type Cluster struct {
		Region  string
		Servers []Server
	}
	type Inventory struct {
		Clusters []Cluster
	}

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(Inventory{Clusters: []Cluster{
		{Region: "eu-west", Servers: []Server{
			{Host: "web-1", Status: "active", Tags: []string{"web", "prod"}},
			{Host: "db-1", Status: "draining", Tags: []string{"db", "prod"}},
		}},
		{Region: "us-east", Servers: []Server{
			{Host: "web-2", Status: "active", Tags: []string{"web", "staging"}},
			{Host: "cache-1", Status: "active", Tags: []string{"cache", "prod"}},
		}},
	}}); err != nil {
		log.Fatal(err)
	}

	values, err := gobspect.New().Stream(&buf).Collect()
	if err != nil {
		log.Fatal(err)
	}
	root := values[0]

	// "..Host" collects every Host field at any depth.
	for _, v := range query.All(root, "..Host") {
		fmt.Println(gobspect.Format(v))
	}

	// Wildcard descent with filters: keep nodes tagged "prod" whose
	// Status is "active", then project just the fields we care about.
	for _, v := range query.All(root, "..[Tags~prod][Status=active].Host,Status") {
		fmt.Println(gobspect.Format(v))
	}
	// snippet:end

	// Output:
	// "web-1"
	// "db-1"
	// "web-2"
	// "cache-1"
	// gobspect:projection{
	//   Host: "web-1"
	//   Status: "active"
	// }
	// gobspect:projection{
	//   Host: "cache-1"
	//   Status: "active"
	// }
}
