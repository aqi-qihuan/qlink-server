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

// StartUpdateLinkListener consumes from short_link.update.link.queue.
func StartUpdateLinkListener(rmq *mq.RabbitMQ, svc *service.ShortLinkService) {
	handler := func(body []byte) error {
		var eventMsg model.EventMessage
		if err := json.Unmarshal(body, &eventMsg); err != nil {
			log.Printf("[MQ] update_link unmarshal error: %v", err)
			return err
		}
		eventMsg.EventMessageType = string(enums.SHORT_LINK_UPDATE_LINK)
		log.Printf("[MQ] consuming update_link, messageId=%s", eventMsg.MessageId)
		if ok := svc.HandleUpdateShortLink(&eventMsg); !ok {
			return fmt.Errorf("update_link handler failed for messageId=%s", eventMsg.MessageId)
		}
		return nil
	}
	if err := rmq.Consume(config.QueueUpdateLink, "update_link_consumer", handler); err != nil {
		log.Fatalf("start update_link listener: %v", err)
	}
}
