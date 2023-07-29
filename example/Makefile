.PHONY: backend load-list load-create load-complete
backend:
	go run ./backend

storage:
	exit 1

api: idl/service.proto
	protoc --go_out=./api --go_opt=paths=source_relative --go-grpc_out=./api --go-grpc_opt=paths=source_relative idl/service.proto --proto_path=idl

model: idl/model.proto
	protoc --go_out=./model --go_opt=paths=source_relative idl/model.proto --proto_path=idl

load-list:
	go run ./load list html > ./reports/list.html

load-create:
	go run ./load create html > ./reports/create.html

load-complete:
	go run ./load complete html > ./reports/complete.html

