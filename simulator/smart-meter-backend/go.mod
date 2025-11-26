module github.com/robertodantas/lnpay/simulator/smartmeter

go 1.25.4

require (
	github.com/eclipse/paho.mqtt.golang v1.5.1
	github.com/gorilla/websocket v1.5.3
	github.com/robertodantas/lnpay/services/proto v0.0.0
)

require (
	golang.org/x/net v0.44.0 // indirect
	golang.org/x/sync v0.17.0 // indirect
	google.golang.org/protobuf v1.34.1 // indirect
)

replace github.com/robertodantas/lnpay/services/proto => ../../services/proto
