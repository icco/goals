package main

import (
	"context"

	"github.com/BTBurke/twiml"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/go-chi/cors"
	"github.com/icco/gotwilio"
	sdLogging "github.com/icco/logrus-stackdriver-formatter"
)

var (
	log = InitLogging()
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
		w.Write([]byte("ok."))
	})

	log.Fatal(http.ListenAndServe(":"+port, r))
}
