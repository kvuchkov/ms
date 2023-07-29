package handler

import (
	"context"
	"fmt"
	"log"
	"math"

	"github.com/kvuchkov/ms-thesis-grpcp/example/api"
	"github.com/kvuchkov/ms-thesis-grpcp/example/model"
	"github.com/oklog/ulid/v2"
	"go.etcd.io/bbolt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type Order struct {
	api.UnimplementedOrderServiceServer
	Db *bbolt.DB
}

func (h *Order) ListOrders(ctx context.Context, rq *api.ListOrdersRequest) (*api.ListOrdersResponse, error) {
	var orders []*model.Order
	var last *string
	err := h.Db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("orders"))
		if b == nil {
			return fmt.Errorf("bucket not found")
		}
		c := b.Cursor()
		var key, value []byte
		if rq.PageToken != nil {
			key, value = c.Seek([]byte(*rq.PageToken))
		} else {
			key, value = c.First()
		}

		for ; key != nil; key, value = c.Next() {
			order := &model.Order{}
			err := proto.Unmarshal(value, order)
			if err != nil {
				return fmt.Errorf("cannot unmarshal order: %w", err)
			}
			if rq.CustomerId != nil && order.CustomerId != *rq.CustomerId {
				continue
			}
			orders = append(orders, order)
			if len(orders) == int(rq.PageSize) {
				key, _ := c.Next()
				if key != nil {
					last = proto.String(string(key))
				}
				break
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("cannot read orders from db: %w", err)
	}
	if len(orders) == 0 {
		return nil, status.Errorf(codes.NotFound, "no orders found")
	}
	return &api.ListOrdersResponse{Orders: orders, NextPageToken: last}, nil
}
func (h *Order) GetOrder(ctx context.Context, rq *api.GetOrderRequest) (order *model.Order, err error) {
	err = h.Db.View(func(tx *bbolt.Tx) error {
		order, err = getOrder(tx, rq.GetId())
		return err
	})
	if order == nil {
		return nil, status.Errorf(codes.NotFound, "order not found")
	}
	return order, err
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
	err = h.Db.Update(func(tx *bbolt.Tx) error {
		idx, err := tx.CreateBucketIfNotExists([]byte("order_customer"))
		if err != nil {
			return fmt.Errorf("cannot create index bucket: %w", err)
		}
		b, err := tx.CreateBucketIfNotExists([]byte("orders"))
		if err != nil {
			return fmt.Errorf("cannot create data bucket: %w", err)
		}
		data, err := proto.Marshal(order)
		if err != nil {
			return fmt.Errorf("cannot serialize order: %w", err)
		}
		err = b.Put(key(order.CustomerId, order.Id), data)
		if err != nil {
			return fmt.Errorf("cannot put order: %w", err)
		}
		err = idx.Put([]byte(order.Id), []byte(order.CustomerId))
		if err != nil {
			return fmt.Errorf("cannot put index: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("cannot create order: %w", err)
	}
	return order, nil
}
func (h *Order) CompleteOrder(ctx context.Context, rq *api.CompleteOrderRequest) (order *model.Order, err error) {
	err = h.Db.Update(func(tx *bbolt.Tx) error {
		order, err = getOrder(tx, rq.GetId())
		if err != nil {
			return err
		}
		order.Status = model.OrderStatus_ORDER_STATUS_COMPLETED
		data, err := proto.Marshal(order)
		if err != nil {
			return fmt.Errorf("cannot serialize order: %w", err)
		}
		return tx.Bucket([]byte("orders")).Put(key(order.CustomerId, order.Id), data)
	})
	if err != nil {
		return nil, fmt.Errorf("cannot read order from db: %w", err)
	} else if order == nil {
		return nil, status.Errorf(codes.NotFound, "order not found")
	}
	return order, nil
}
func key(customerID, orderID string) []byte {
	return []byte(customerID + "/" + orderID)
}
func getOrder(tx *bbolt.Tx, id string) (*model.Order, error) {
	customerId := tx.Bucket([]byte("order_customer")).Get([]byte(id))
	if customerId == nil {
		return nil, nil
	}
	value := tx.Bucket([]byte("orders")).Get(key(string(customerId), id))
	if value == nil {
		return nil, nil
	}
	order := &model.Order{}
	err := proto.Unmarshal(value, order)
	if err != nil {
		return nil, fmt.Errorf("cannot unmarshal order: %w", err)
	}
	return order, nil
}
