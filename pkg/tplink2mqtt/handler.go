// Package tplink2mqtt contains a handler for dealing with tplink2mqtt messages
package tplink2mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/shauncampbell/tplink2mqtt/pkg/tplink"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Handler handles tplink2mqtt messages
type Handler struct {
	database *mongo.Database
	logger   zerolog.Logger
	devices  map[string]*tplink.Device
}

// Connected is a handler which is called when the initial connection to the mqtt server is established.
func (h *Handler) Connected(client mqtt.Client) {
	token := client.Subscribe("tplink2mqtt/bridge/devices", 1, h.deviceListChangedHandler)
	if token.Wait() && token.Error() != nil {
		fmt.Println(token.Error())
	}
}

// DefaultPublished is a handler which is called when a message is received but no explicit handler is called.
func (h *Handler) DefaultPublished(client mqtt.Client, message mqtt.Message) {
	h.logger.Info().Msgf("received message on topic: %s", message.Topic())
}

func (h *Handler) deviceListChangedHandler(client mqtt.Client, message mqtt.Message) {
	var devices []tplink.Device
	err := json.Unmarshal(message.Payload(), &devices)
	if err != nil {
		fmt.Println(err.Error())
	} else {
		for i := range devices {
			device := &devices[i]
			if h.devices[device.ID] == nil {
				h.subscribeToDevice(client, device)
			}
		}
	}
}

func sanitizeFriendlyName(friendlyName string) string {
	str := strings.ToLower(friendlyName)
	str = strings.ReplaceAll(str, " ", "_")
	return str
}

func (h *Handler) subscribeToDevice(client mqtt.Client, device *tplink.Device) {
	logger := h.logger.With().Str("friendly_name", device.Info.FriendlyName).Str("device_id", device.ID).Logger()
	logger.Info().Msgf("subscribing to device")
	client.Subscribe("tplink2mqtt/"+sanitizeFriendlyName(device.Info.FriendlyName), 1, h.deviceEventHandler(device, &logger))
	h.persistDeviceToMongodb(device)
	h.devices[device.ID] = device
}

func (h *Handler) deviceEventHandler(device *tplink.Device, logger *zerolog.Logger) mqtt.MessageHandler {
	return func(client mqtt.Client, message mqtt.Message) {
		logger.Info().Msgf("received new status message")
		var m map[string]interface{}
		err := json.Unmarshal(message.Payload(), &m)
		if err != nil {
			logger.Error().Msgf("failed to unmarshall payload: %s", err.Error())
			return
		}

		h.persistStateToMongoDB(device, m)
	}
}

func (h *Handler) persistDeviceToMongodb(device *tplink.Device) {
	// Set up mongodb variables
	collection := h.database.Collection("current_state")

	set := bson.D{
		{Key: "friendly_name", Value: device.Info.FriendlyName},
		{Key: "type", Value: "hs1xx"},
		{Key: "network_address", Value: device.Info.NetworkAddress},
		{Key: "model", Value: device.Info.Model},
		{Key: "vendor", Value: device.Info.Vendor},
		{Key: "attributes", Value: device.Info.Exposes},
	}

	filter := bson.D{{Key: "_id", Value: "tplink_" + device.ID}}
	_, err := collection.UpdateOne(context.Background(), filter, bson.D{{Key: "$set", Value: set}}, options.Update().SetUpsert(true))
	if err != nil {
		h.logger.Error().Msgf("failed to persist to mongodb: %s", err.Error())
	}
}

func (h *Handler) persistStateToMongoDB(device *tplink.Device, state map[string]interface{}) {
	// Figure out the current time
	t := time.Now().UTC()
	y, m, d := time.Now().UTC().Date()
	ts := t.Unix()

	// Set up mongodb variables
	collection := h.database.Collection("current_state")
	set := bson.D{{Key: "update_ts", Value: ts}, {Key: "friendly_name", Value: device.Info.FriendlyName}}
	push := bson.D{}

	// Iterate through the changes.
	for _, attr := range device.Info.Exposes {
		if state[attr.Property] != nil {
			set = append(set, bson.E{Key: attr.Property, Value: state[attr.Property]})
			push = append(push, bson.E{Key: attr.Property, Value: bson.D{{Key: "ts", Value: ts}, {Key: "v", Value: state[attr.Property]}}})
		}
	}

	filter := bson.D{{Key: "_id", Value: "tplink_" + device.ID}}
	_, err := collection.UpdateOne(context.Background(), filter, bson.D{{Key: "$set", Value: set}}, options.Update().SetUpsert(true))
	if err != nil {
		h.logger.Error().Msgf("failed to persist to mongodb: %s", err.Error())
	}

	collection = h.database.Collection("historical_state")
	_, err = collection.Indexes().CreateOne(
		context.Background(),
		mongo.IndexModel{
			Keys: bson.D{
				bson.E{Key: "ieee_address", Value: 1},
				bson.E{Key: "y", Value: 1},
				bson.E{Key: "m", Value: 1},
				bson.E{Key: "d", Value: 1},
			},
		},
	)
	if err != nil {
		h.logger.Error().Msgf("failed to create index in mongodb: %s", err.Error())
	}

	filter = bson.D{
		{Key: "ieee_address", Value: device.ID},
		{Key: "friendly_name", Value: device.Info.FriendlyName},
		{Key: "y", Value: y},
		{Key: "m", Value: m},
		{Key: "d", Value: d},
	}
	_, err = collection.UpdateOne(context.Background(), filter, bson.D{{Key: "$push", Value: push}}, options.Update().SetUpsert(true))
	if err != nil {
		h.logger.Error().Msgf("failed to persist to mongodb: %s", err.Error())
	}
}

// Disconnected is called when the client disconnects from mqtt.
func (h *Handler) Disconnected(client mqtt.Client, err error) {
	h.logger.Error().Msgf("disconnected from mqtt: %s", err.Error())
}

// New creates a new handler.
func New(client *mongo.Client) *Handler {
	return &Handler{devices: make(map[string]*tplink.Device), logger: log.Logger, database: client.Database("home")}
}
