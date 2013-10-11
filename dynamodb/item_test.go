package dynamodb_test

import (
	"github.com/hailocab/goamz/aws"
	"github.com/hailocab/goamz/dynamodb"
	"testing"
)

func dynamodbServerSetup(t *testing.T) (dynamodb.Server) {
	auth, err := aws.EnvAuth()

	if err != nil {
		t.Log(err)
		t.FailNow()
	}

	return dynamodb.Server{auth, aws.EUWest}
}

func TestPutItem(t *testing.T) {
	server := dynamodbServerSetup(t)
	key := dynamodb.PrimaryKey{dynamodb.NewStringAttribute("id", ""), nil}
	table := server.NewTable("gotest", key)
	
	item := dynamodb.NewItem()
	item.AddAttribute(dynamodb.NewNumericAttribute("id", "1"))
	item.AddAttribute(dynamodb.NewStringAttribute("description", "lorem"))
	
	result, err := table.PutItem(item)
	if result != true {
		t.Fatalf("Error from table.PutItem: %#v", result)
	}
	
	if err != nil {
		t.Fatalf("Error from table.PutItem: %#v", err)
	}
}

func TestBatchWriteItem(t *testing.T) {
	server := dynamodbServerSetup(t)
	key := dynamodb.PrimaryKey{dynamodb.NewStringAttribute("id", ""), nil}
	table := server.NewTable("gotest", key)
	
	item1 := dynamodb.NewItem()
	item1.AddAttribute(dynamodb.NewNumericAttribute("id", "1"))
	item1.AddAttribute(dynamodb.NewStringAttribute("description", "lorem1"))
	
	item2 := dynamodb.NewItem()
	item2.AddAttribute(dynamodb.NewNumericAttribute("id", "2"))
	item2.AddAttribute(dynamodb.NewStringAttribute("description", "lorem2"))
	
	request := dynamodb.NewBatchWriteItemRequest()
	request.AddPutRequest("gotest", item1)
	request.AddPutRequest("gotest", item2)
	
	_, err := table.BatchWriteItem(request)
	
	if err != nil {
		t.Fatalf("Error from table.BatchWriteItem: %#v", err)
	}
}

