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

// StartDelLinkListener consumes from short_link.del.link.queue.
func StartDelLinkListener(rmq *mq.RabbitMQ, svc *service.ShortLinkService) {
	handler := func(body []byte) error {
		var eventMsg model.EventMessage
		if err := json.Unmarshal(body, &eventMsg); err != nil {
			log.Printf("[MQ] del_link unmarshal error: %v", err)
			return err
		}
		eventMsg.EventMessageType = string(enums.SHORT_LINK_DEL_LINK)
		log.Printf("[MQ] consuming del_link, messageId=%s", eventMsg.MessageId)
		if ok := svc.HandleDelShortLink(&eventMsg); !ok {
			return fmt.Errorf("del_link handler failed for messageId=%s", eventMsg.MessageId)
		}
		return nil
	}
	if err := rmq.Consume(config.QueueDelLink, "del_link_consumer", handler); err != nil {
		log.Fatalf("start del_link listener: %v", err)
	}
}
