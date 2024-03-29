package main

import (
	"fmt"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/philomusica/tickets-lambda-utils/lib/databaseHandler"
	"github.com/philomusica/tickets-lambda-utils/lib/emailHandler"
	"github.com/philomusica/tickets-lambda-utils/lib/paymentHandler"
)

/*
func TestMain(m *testing.M) {
	rc := m.Run()
	if rc == 0 && testing.CoverMode() != "" {
		c := testing.Coverage()
		fmt.Println(c)
		if c < 0.9 {
			fmt.Printf("Tests passed but coverage was below %d%%\n", int(c*100))
			rc = -1
		}
	}
	os.Exit(rc)
}
*/

// ===============================================================================================================================
// PARSE_REQUEST_BODY TESTS
// ===============================================================================================================================

func TestParseRequestBodySuccess(t *testing.T) {
	request := `
		{
			"orderLines": [
				{
					"numOfFullPrice": 1,
					"numOfConcessions": 2,
					"concertId": "ABC"
				}
			]
		}
	`
	var pr paymentHandler.PaymentRequest
	err := parseRequestBody(request, &pr)
	if err != nil {
		t.Errorf("Expected no error, got %s", err)
	}
}

func TestParseRequestBodyNoBody(t *testing.T) {
	request := ""
	var pr paymentHandler.PaymentRequest
	err := parseRequestBody(request, &pr)
	errMessage, ok := err.(ErrInvalidRequestBody)
	if !ok {
		t.Errorf("Expected err: '%s', got '%s'", err.(ErrInvalidRequestBody), errMessage)
	}
}

func TestParseRequestBodyInvalidFullPrice(t *testing.T) {
	request := `
		{
			"numOfFullPrice": "bob",
			"numOfConcessions": 2,
			"concertId": "ABC"
		}
	`
	var pr paymentHandler.PaymentRequest
	err := parseRequestBody(request, &pr)
	errMessage, ok := err.(ErrInvalidRequestBody)
	if !ok {
		t.Errorf("Expected err: '%s', got '%s'", err.(ErrInvalidRequestBody), errMessage)
	}
}

func TestParseRequestBodyNoConcessionPrice(t *testing.T) {
	request := `
		"orderLines": [
			{
				"numOfFullPrice": 2,
				"concertId": "ABC"
			}
		]
	`
	var pr paymentHandler.PaymentRequest
	err := parseRequestBody(request, &pr)
	errMessage, ok := err.(ErrInvalidRequestBody)
	if !ok {
		t.Errorf("Expected err: '%s', got '%s'", err.(ErrInvalidRequestBody), errMessage)
	}
}

func TestParseRequestBodyInvalidConcertID(t *testing.T) {
	request := `
		{
			"numOfFullPrice": 2,
			"numOfConcessions": 2,
			"concertId": 2
		}
	`
	var pr paymentHandler.PaymentRequest
	err := parseRequestBody(request, &pr)
	errMessage, ok := err.(ErrInvalidRequestBody)
	if !ok {
		t.Errorf("Expected err: '%s', got '%s'", err.(ErrInvalidRequestBody), errMessage)
	}
}

// ===============================================================================================================================
// END PARSE_REQUEST_BODY TESTS
// ===============================================================================================================================

// ===============================================================================================================================
// PROCESS_PAYMENT TESTS
// ===============================================================================================================================

type mockDDBHandlerConcertInPast struct {
	databaseHandler.DatabaseHandler
}

func (m mockDDBHandlerConcertInPast) GetConcertFromTable(concertID string) (concert *databaseHandler.Concert, err error) {
	err = databaseHandler.ErrConcertInPast{Message: "Error concert x in the past, tickets are no longer avaiable"}
	return
}

type mockStripeHandlerEmpty struct{}

func (m mockStripeHandlerEmpty) Process(balance float32, reference string) (clientSecret string, err error) {
	return
}

type mockEmailHandler struct {
	emailHandler.EmailHandler
}

func TestProcessPaymentConcertInPast(t *testing.T) {

	request := events.APIGatewayProxyRequest{
		Body: `{
			"orderLines": [
				{
					"numOfFullPrice": 2,
					"numOfConcessions": 2,
					"concertId": "ABC"
				}
			]
		}`,
	}

	mockDyanmoHanlder := mockDDBHandlerConcertInPast{}
	mockStripeHandler := mockStripeHandlerEmpty{}
	response := processPayment(request, mockDyanmoHanlder, mockStripeHandler)

	expectedStatusCode := 400
	concertInPastErrMesage := "Error concert x in the past, tickets are no longer avaiable"
	if response.StatusCode != expectedStatusCode || response.Body != concertInPastErrMesage {
		t.Errorf("Expected statusCode %d and Body %s, got %d and %s", expectedStatusCode, concertInPastErrMesage, response.StatusCode, response.Body)
	}
}

type mockDDBHandlerInsufficientTickets struct {
	databaseHandler.DatabaseHandler
}

func (m mockDDBHandlerInsufficientTickets) GetConcertFromTable(concertID string) (concert *databaseHandler.Concert, err error) {
	concert = &databaseHandler.Concert{
		AvailableTickets: 9,
		Title:            "summer concert",
	}
	return
}

func TestProcessPaymentInsufficientTicketsAvailable(t *testing.T) {
	request := events.APIGatewayProxyRequest{
		Body: `{
			"orderLines": [
				{
					"numOfFullPrice": 8,
					"numOfConcessions": 2,
					"concertId": "ABC"
				}
			]
		}`,
	}

	mockDynamoHandler := mockDDBHandlerInsufficientTickets{}
	mockStripeHandler := mockStripeHandlerEmpty{}
	response := processPayment(request, mockDynamoHandler, mockStripeHandler)

	expectedStatusCode := 403
	expectedResponseBody := fmt.Sprintf("Insufficient tickets available for %s\n", "summer concert")
	if response.StatusCode != expectedStatusCode || response.Body != expectedResponseBody {
		t.Errorf("Expected statusCode %d and Body %s, got %d and %s", expectedStatusCode, expectedResponseBody, response.StatusCode, response.Body)
	}
}

// ===============================================================================================================================
// END PROCESS_PAYMENT TESTS
// ===============================================================================================================================

// ===============================================================================================================================
// HANDLER TESTS
// ===============================================================================================================================

func TestHandlerEnvironmentVariablesNotSet(t *testing.T) {
	request := events.APIGatewayProxyRequest{}
	response, _ := Handler(request)
	expectedStatusCode := 500
	expectedBody := DEFAULT_JSON_RESPONSE
	if response.StatusCode != expectedStatusCode || response.Body != expectedBody {
		t.Errorf("Expected status code %d and body %s, got %d and %s\n", response.StatusCode, response.Body, expectedStatusCode, expectedBody)
	}
}

func TestHandlerInvalidRequest(t *testing.T) {
	request := events.APIGatewayProxyRequest{
		Body: `{
			"orderLines": [
				{
					"numOfConcessions": 2,
					"concertId": "ABC"	
				}
			]
		}`,
	}
	t.Setenv("CONCERTS_TABLE", "concerts-table")
	t.Setenv("ORDERS_TABLE", "orders-table")

	response, _ := Handler(request)
	expectedStatusCode := 500
	expectedResponseBody := DEFAULT_JSON_RESPONSE
	if response.StatusCode != expectedStatusCode || response.Body != expectedResponseBody {
		t.Errorf("Expected statusCode %d and Body %s, got %d and %s", expectedStatusCode, expectedResponseBody, response.StatusCode, response.Body)
	}
}

// ===============================================================================================================================
// END HANDLER TESTS
// ===============================================================================================================================
