package main

import (
	"context"
	"net/http"
	"os"

	"github.com/BTBurke/twiml"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	sdLogging "github.com/icco/logrus-stackdriver-formatter"
)

var (
	log = InitLogging()
)

// "github.com/icco/gotwilio"
// func SendMessage(ctx context.Context, to, goal string) error {
// 	accountSid := "ABC123..........ABC123"
// 	authToken := "ABC123..........ABC123"
// 	twilio := gotwilio.NewTwilioClient(accountSid, authToken)
//
// 	// https://www.twilio.com/console/phone-numbers/PN87a39ded4c2f6ba4b938f2a7c0d46579
// 	from := "+17073294103"
// 	message := fmt.Sprintf("Hi, did you complete your goal of \"%s\" yesterday?", goal)
// 	resp, err := twilio.SendSMS(from, to, message, "", applicationSid)
// 	if err != nil {
// 		return err
// 	}
//
// 	log.Infof("sent %+v", resp)
// 	return nil
// }

func RecieveMessage(ctx context.Context, msg twiml.Sms) error {
	log.Infof("recieved %+v", msg)
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
		w.Write([]byte("ok."))
	})

	r.Post("/sms", func(w http.ResponseWriter, r *http.Request) {
		var sms twiml.Sms
		if err := twiml.Bind(&sms, r); err != nil {
			log.WithError(err).Error("couldn't parse")
			http.Error(w, http.StatusText(400), 400)
			return
		}

		err := RecieveMessage(r.Context(), sms)
		if err != nil {
			log.WithError(err).Error("couldn't recieve")
			http.Error(w, http.StatusText(400), 400)
			return
		}
	})

	log.Fatal(http.ListenAndServe(":"+port, r))
}
