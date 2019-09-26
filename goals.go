package goals

import (
	"context"

	"github.com/BTBurke/twiml"
	"github.com/icco/gotwilio"
)

func SendMessage(ctx context.Context, to, goal string) error {
	accountSid := "ABC123..........ABC123"
	authToken := "ABC123..........ABC123"
	twilio := gotwilio.NewTwilioClient(accountSid, authToken)

	from := "+15555555555"
	message := fmt.Sprintf("Hi, did you complete your goal of \"%s\" yesterday?", goal)
	resp, err := twilio.SendSMS(from, to, message, "", applicationSid)
	if err != nil {
		return err
	}

	log.Infof("sent %+v", resp)
	return nil
}

func RecieveMessage(ctx context.Context, msg twilml.Sms) error {

}
