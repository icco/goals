package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/icco/gotwilio"
	sdLogging "github.com/icco/logrus-stackdriver-formatter"
	"github.com/sirupsen/logrus"
)

var (
	log = InitLogging()
)

// SendMessage texts a message.
func SendMessage(ctx context.Context, to, goal string) error {
	accountSid := os.Getenv("TWILIO_ACCOUNT_SID")
	applicationSid := os.Getenv("TWILIO_APPLICATION_SID")
	authToken := os.Getenv("TWILIO_AUTH_TOKEN")
	twilio := gotwilio.NewTwilioClient(accountSid, authToken)

	// https://www.twilio.com/console/phone-numbers/PN87a39ded4c2f6ba4b938f2a7c0d46579
	from := "+17073294103"
	message := fmt.Sprintf("Hi, did you complete your goal of \"%s\" yesterday?", goal)
	resp, exp, err := twilio.SendSMS(from, to, message, "", applicationSid)
	if err != nil {
		return err
	}

	if exp != nil {
		return fmt.Errorf(exp.Error())
	}

	log.WithFields(logrus.Fields{"response": resp, "message": message}).Info("sent")
	return nil
}

// RecieveMessage parses a twilio message.
func RecieveMessage(ctx context.Context, msg gotwilio.SMSWebhook) error {
	log.WithContext(ctx).WithFields(logrus.Fields{"parsed": msg}).Info("recieved sms")

	messageText := strings.TrimFunc(strings.ToLower(msg.Body), func(r rune) bool {
		return !unicode.IsLetter(r)
	})

	ack := strings.Split(messageText, "")[0] == "y"
	from := msg.From
	when := time.Now()

	return SaveMessageLog(ctx, from, when, ack)
}

// SaveMessageLog is unimplemented, but could write stuff to a db.
func SaveMessageLog(ctx context.Context, from string, when time.Time, ack bool) error {
	return nil
}

func main() {
	port := "8080"
	if fromEnv := os.Getenv("PORT"); fromEnv != "" {
		port = fromEnv
	}
	log.Printf("Starting up on http://localhost:%s", port)

	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(sdLogging.LoggingMiddleware(log))

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`hi! Please see <a href="https://github.com/icco/goals">github.com/icco/goals</a> for more information.`))
	})

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok."))
	})

	r.Get("/cron", func(w http.ResponseWriter, r *http.Request) {
		goals := [][]string{
			[]string{"+17077998675", "10k steps"},
			[]string{"+15125788969", "exercise, floss and no alcohol"},
		}

		for _, goal := range goals {
			err := SendMessage(r.Context(), goal[0], goal[1])
			if err != nil {
				log.WithError(err).Error("couldn't send")
				http.Error(w, http.StatusText(500), 500)
				return
			}
		}

		w.Write([]byte("ok."))
	})

	r.Post("/sms", func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseForm()
		if err != nil {
			log.WithError(err).Error("couldn't parse")
			http.Error(w, http.StatusText(400), 400)
			return
		}

		var sms gotwilio.SMSWebhook
		err = gotwilio.DecodeWebhook(r.PostForm, &sms)
		if err != nil {
			log.WithError(err).Error("couldn't decode")
		}

		err = RecieveMessage(r.Context(), sms)
		if err != nil {
			log.WithError(err).Error("couldn't recieve")
			http.Error(w, http.StatusText(400), 400)
			return
		}
	})

	log.Fatal(http.ListenAndServe(":"+port, r))
}
