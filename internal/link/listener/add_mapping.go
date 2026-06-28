package listener

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/aqi/qlink-server/internal/common/enums"
	"github.com/aqi/qlink-server/internal/common/model"
	"github.com/aqi/qlink-server/internal/common/mq"
	"github.com/aqi/qlink-server/internal/link/config"
	"github.com/aqi/qlink-server/internal/link/service"
)

// StartAddMappingListener consumes from short_link.add.mapping.queue.
func StartAddMappingListener(rmq *mq.RabbitMQ, svc *service.ShortLinkService) {
	handler := func(body []byte) error {
		var eventMsg model.EventMessage
		if err := json.Unmarshal(body, &eventMsg); err != nil {
			log.Printf("[MQ] add_mapping unmarshal error: %v", err)
			return err
		}
		eventMsg.EventMessageType = string(enums.SHORT_LINK_ADD_MAPPING)
		log.Printf("[MQ] consuming add_mapping, messageId=%s", eventMsg.MessageId)
		if ok := svc.HandleAddShortLink(&eventMsg); !ok {
			return fmt.Errorf("add_mapping handler failed for messageId=%s", eventMsg.MessageId)
		}
		return nil
	}
	if err := rmq.Consume(config.QueueAddMapping, "add_mapping_consumer", handler); err != nil {
		log.Fatalf("start add_mapping listener: %v", err)
	}
}
