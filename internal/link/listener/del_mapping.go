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

// StartDelMappingListener consumes from short_link.del.mapping.queue.
func StartDelMappingListener(rmq *mq.RabbitMQ, svc *service.ShortLinkService) {
	handler := func(body []byte) error {
		var eventMsg model.EventMessage
		if err := json.Unmarshal(body, &eventMsg); err != nil {
			log.Printf("[MQ] del_mapping unmarshal error: %v", err)
			return err
		}
		eventMsg.EventMessageType = string(enums.SHORT_LINK_DEL_MAPPING)
		log.Printf("[MQ] consuming del_mapping, messageId=%s", eventMsg.MessageId)
		if ok := svc.HandleDelShortLink(&eventMsg); !ok {
			return fmt.Errorf("del_mapping handler failed for messageId=%s", eventMsg.MessageId)
		}
		return nil
	}
	if err := rmq.Consume(config.QueueDelMapping, "del_mapping_consumer", handler); err != nil {
		log.Fatalf("start del_mapping listener: %v", err)
	}
}
