module github.com/robertodantas/lnpay/simulator/smartmeter

go 1.25.4

require (
	github.com/eclipse/paho.mqtt.golang v1.5.1
	github.com/gorilla/websocket v1.5.3
	github.com/robertodantas/lnpay/internal v0.0.0
	github.com/robertodantas/lnpay/services/proto v0.0.0
	google.golang.org/protobuf v1.34.1
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/redis/go-redis/v9 v9.17.0 // indirect
	golang.org/x/net v0.44.0 // indirect
	golang.org/x/sync v0.17.0 // indirect
	golang.org/x/sys v0.36.0 // indirect
	golang.org/x/text v0.29.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240604185151-ef581f913117 // indirect
	google.golang.org/grpc v1.66.0 // indirect
)

replace github.com/robertodantas/lnpay/services/proto => ../../services/proto

replace github.com/robertodantas/lnpay/internal => ../../services/internal
