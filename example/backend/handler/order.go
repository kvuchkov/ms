package handler

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"

	"github.com/cockroachdb/pebble"
	"github.com/kvuchkov/ms-thesis-grpcp/example/api"
	"github.com/kvuchkov/ms-thesis-grpcp/example/model"
	"github.com/oklog/ulid/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type Order struct {
	api.UnimplementedOrderServiceServer
	DB *pebble.DB
}

func (h *Order) ListOrders(ctx context.Context, rq *api.ListOrdersRequest) (*api.ListOrdersResponse, error) {
	var orders []*model.Order
	var last *string
	var lb []byte
	if rq.PageToken != nil {
		lb = []byte(*rq.PageToken)
	} else if rq.CustomerId != nil {
		lb = key(*rq.CustomerId, "")
	} else {
		lb = []byte("order/")
	}
	it := h.DB.NewIter(&pebble.IterOptions{
		LowerBound: lb,
	})
	defer it.Close()
	var more bool
	for more = it.First(); more; more = it.Next() {
		order := &model.Order{}
		value, err := it.ValueAndErr()
		if err != nil {
			return nil, fmt.Errorf("cannot iterate over orders: %w", err)
		}
		err = proto.Unmarshal(value, order)
		if err != nil {
			return nil, fmt.Errorf("cannot unmarshal order: %w", err)
		}
		if rq.CustomerId != nil && order.CustomerId != *rq.CustomerId {
			continue
		}
		orders = append(orders, order)
		if len(orders) == int(rq.PageSize) {
			if it.Next() {
				last = proto.String(string(it.Key()))
			}
			break
		}
	}
	if len(orders) == 0 {
		return nil, status.Errorf(codes.NotFound, "no orders found")
	}
	return &api.ListOrdersResponse{Orders: orders, NextPageToken: last}, nil
}
func (h *Order) GetOrder(ctx context.Context, rq *api.GetOrderRequest) (*model.Order, error) {
	order, err := h.getOrder(rq.Id)
	if errors.Is(err, pebble.ErrNotFound) {
		return nil, status.Errorf(codes.NotFound, "order not found")
	} else if err != nil {
		return nil, fmt.Errorf("cannot get order: %w", err)
	}
	return order, nil
}
func (h *Order) CreateOrder(ctx context.Context, rq *api.CreateOrderRequest) (*model.Order, error) {
	var orderID ulid.ULID
	var err error
	if orderID, err = ulid.Parse(rq.Id); err != nil {
		log.Println("Bad order ID", rq.Id, err)
		return nil, status.Error(codes.InvalidArgument, "invalid order id: must be a valid ulid")
	}
	currency := rq.Items[0].PricePerUnit.CurrencyCode
	order := &model.Order{
		Id:         orderID.String(),
		CustomerId: rq.CustomerId,
		Status:     model.OrderStatus_ORDER_STATUS_NEW,
		Shipment:   &model.Shipment{MethodId: rq.Shipment.MethodId, Price: rq.Shipment.Price},
		Payment:    rq.GetPayment(),
		Items:      rq.Items,
		Breakdown: &model.Breakdown{
			Subtotal: &model.Money{CurrencyCode: currency},
			Tax:      &model.Money{CurrencyCode: currency},
			Total:    &model.Money{CurrencyCode: currency},
		},
	}
	for _, item := range order.Items {
		order.Breakdown.Subtotal.AmountE2 += int32(item.PricePerUnit.AmountE2) * item.Quantity
	}
	order.Breakdown.Tax.AmountE2 = int32(math.Round(float64(order.Breakdown.Subtotal.AmountE2) * 0.2))
	order.Breakdown.Total.AmountE2 = order.Breakdown.Subtotal.AmountE2 + order.Breakdown.Tax.AmountE2
	data, err := proto.Marshal(order)
	if err != nil {
		return nil, fmt.Errorf("cannot serialize order: %w", err)
	}
	batch := h.DB.NewBatch()
	batch.Set(key(order.CustomerId, order.Id), data, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot create order: %w", err)
	}

	err = batch.Set([]byte("customer/"+order.Id), []byte(order.CustomerId), nil)
	if err != nil {
		return nil, fmt.Errorf("cannot index order: %w", err)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot create order: %w", err)
	}
	if err = batch.Commit(pebble.Sync); err != nil {
		return nil, fmt.Errorf("cannot commit order: %w", err)
	}
	return order, nil
}
func (h *Order) CompleteOrder(ctx context.Context, rq *api.CompleteOrderRequest) (*model.Order, error) {
	order, err := h.getOrder(rq.Id)
	if errors.Is(err, pebble.ErrNotFound) {
		return nil, status.Errorf(codes.NotFound, "order not found")
	} else if err != nil {
		return nil, fmt.Errorf("cannot get order: %w", err)
	}
	order.Status = model.OrderStatus_ORDER_STATUS_COMPLETED
	data, err := proto.Marshal(order)
	if err != nil {
		return nil, fmt.Errorf("cannot serialize order: %w", err)
	}
	err = h.DB.Set(key(order.CustomerId, order.Id), data, pebble.Sync)
	if err != nil {
		return nil, fmt.Errorf("cannot complete order: %w", err)
	}
	return order, nil
}
func key(customerID, orderID string) []byte {
	return []byte("order/" + customerID + "/" + orderID)
}
func (h *Order) getOrder(id string) (*model.Order, error) {
	// loookup
	indexValue, closer, err := h.DB.Get([]byte("customer/" + id))
	if err != nil {
		return nil, fmt.Errorf("cannot get order: %w", err)
	}
	orderKey := key(string(indexValue), id)
	closer.Close()

	// load
	value, closer, err := h.DB.Get(orderKey)
	if err != nil {
		return nil, fmt.Errorf("cannot get order: %w", err)
	}
	order := &model.Order{}
	err = proto.Unmarshal(value, order)
	if err != nil {
		return nil, fmt.Errorf("cannot unmarshal order: %w", err)
	}
	return order, nil
}
