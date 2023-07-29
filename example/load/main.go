package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/bojand/ghz/printer"
	"github.com/bojand/ghz/runner"
	"github.com/jhump/protoreflect/dynamic"
	"github.com/kvuchkov/ms-thesis-grpcp/example/api"
	"github.com/kvuchkov/ms-thesis-grpcp/example/model"
	"github.com/oklog/ulid/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var customers = []string{
	"53a1366f206a",
	"7f3afa68838e",
	"145e213fccc8",
	"32d705e2ba1c",
	"08a29e9b36e9",
	"27b8ffdf231e",
	"fb3ca8b94131",
	"06cb69a4289c",
	"68e657ff2b78",
	"06dcad7b4308",
}

const (
	total       = 1000
	rps         = 100
	concurrency = 2
)

func main() {
	if len(os.Args) != 3 {
		log.Fatal("Usage: load [list|create|complete] [format]")
	}
	switch os.Args[1] {
	case "create":
		create(os.Args[2])
	case "complete":
		complete(os.Args[2])
	case "list":
		list(os.Args[2])
	default:
		log.Fatal("Unknown command ", os.Args[1])
	}
}

func create(format string) {
	report, err := runner.Run(
		"example.api.OrderService.CreateOrder",
		"localhost:8001",
		runner.WithProtoFile("./idl/service.proto", []string{"./idl"}),
		runner.WithInsecure(true),
		runner.WithDataProvider(generateCreate),
		runner.WithTotalRequests(total),
		runner.WithRPS(rps),
		runner.WithConcurrency(concurrency),
	)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	printer := printer.ReportPrinter{
		Out:    os.Stdout,
		Report: report,
	}

	printer.Print(format)
}

func complete(format string) {
	conn, err := grpc.Dial("localhost:8001", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	client := api.NewOrderServiceClient(conn)
	var orderIds []string
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	var pageToken *string
	for {
		res, err := client.ListOrders(ctx, &api.ListOrdersRequest{
			PageSize:  100,
			PageToken: pageToken,
		})
		if err != nil {
			log.Fatal(err)
		}
		pageToken = res.NextPageToken
		for _, o := range res.Orders {
			orderIds = append(orderIds, o.Id)
		}
		if pageToken == nil {
			break
		}
	}
	cancel()
	err = conn.Close()
	if err != nil {
		log.Fatal(err)
	}

	report, err := runner.Run(
		"example.api.OrderService.CompleteOrder",
		"localhost:8001",
		runner.WithProtoFile("./idl/service.proto", []string{"./idl"}),
		runner.WithInsecure(true),
		runner.WithDataProvider(func(cd *runner.CallData) ([]*dynamic.Message, error) {
			create := &api.CompleteOrderRequest{
				Id: orderIds[int(cd.RequestNumber)],
			}
			dm, err := dynamic.AsDynamicMessage(create)
			return []*dynamic.Message{dm}, err
		}),
		runner.WithTotalRequests(total),
		runner.WithRPS(rps),
		runner.WithConcurrency(concurrency),
	)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	printer := printer.ReportPrinter{
		Out:    os.Stdout,
		Report: report,
	}

	printer.Print(format)
}

func list(format string) {
	report, err := runner.Run(
		"example.api.OrderService.ListOrders",
		"localhost:8001",
		runner.WithProtoFile("./idl/service.proto", []string{"./idl"}),
		runner.WithInsecure(true),
		runner.WithDataProvider(generateList),
		runner.WithTotalRequests(total),
		runner.WithRPS(rps),
		runner.WithConcurrency(concurrency),
	)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	printer := printer.ReportPrinter{
		Out:    os.Stdout,
		Report: report,
	}

	printer.Print(format)
}

func generateCreate(cd *runner.CallData) ([]*dynamic.Message, error) {
	cust := customers[int(cd.RequestNumber)%len(customers)]
	create := &api.CreateOrderRequest{
		Id:         ulid.MustNew(ulid.Now(), ulid.DefaultEntropy()).String(),
		CustomerId: cust,
		Payment: &model.Payment{
			MethodId: "card_" + cust,
		},
		Items: []*model.OrderItem{
			{ProductId: "1", Quantity: 3, PricePerUnit: &model.Money{AmountE2: 315, CurrencyCode: "EUR"}},
			{ProductId: "2", Quantity: 11, PricePerUnit: &model.Money{AmountE2: 1285, CurrencyCode: "EUR"}},
		},
		Shipment: &model.Shipment{
			MethodId: "standard",
		},
	}
	dm, err := dynamic.AsDynamicMessage(create)
	return []*dynamic.Message{dm}, err
}

func generateList(cd *runner.CallData) ([]*dynamic.Message, error) {
	cust := customers[int(cd.RequestNumber)%len(customers)]
	create := &api.ListOrdersRequest{
		PageSize: 10,
	}
	// 90% of the requests are listing orders for specific customer
	if cd.RequestNumber%10 != 0 {
		create.CustomerId = &cust
	}
	dm, err := dynamic.AsDynamicMessage(create)
	return []*dynamic.Message{dm}, err
}
