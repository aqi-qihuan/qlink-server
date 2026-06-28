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

// StartAddLinkListener consumes from short_link.add.link.queue.
func StartAddLinkListener(rmq *mq.RabbitMQ, svc *service.ShortLinkService) {
	handler := func(body []byte) error {
		var eventMsg model.EventMessage
		if err := json.Unmarshal(body, &eventMsg); err != nil {
			log.Printf("[MQ] add_link unmarshal error: %v", err)
			return err
		}
		eventMsg.EventMessageType = string(enums.SHORT_LINK_ADD_LINK)
		log.Printf("[MQ] consuming add_link, messageId=%s", eventMsg.MessageId)
		if ok := svc.HandleAddShortLink(&eventMsg); !ok {
			return fmt.Errorf("add_link handler failed for messageId=%s", eventMsg.MessageId)
		}
		return nil
	}
	if err := rmq.Consume(config.QueueAddLink, "add_link_consumer", handler); err != nil {
		log.Fatalf("start add_link listener: %v", err)
	}
}
