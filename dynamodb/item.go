package dynamodb

import simplejson "github.com/bitly/go-simplejson"
import (
	"errors"
	"fmt"
	"log"
)

type BatchWriteItemRequest struct {
	operations                 map[string]*BatchWriteItemOperations
	returnConsumedCapacity     bool
	returnItemCollecionMetrics bool
}

type BatchWriteItemOperations struct {
	deleteRequest []*Item
	putRequest    []*Item
}

type BatchGetItem struct {
	Server *Server
	Keys   map[*Table][]Key
}

type Item struct {
	attributes []*Attribute
}

func NewItem() *Item {
	return &Item{}
}

func (i *Item) AddAttribute(attribute *Attribute) {
	i.attributes = append(i.attributes, attribute)
}

func (i *Item) AddAttributesFromMap(attributes map[string]*Attribute) {
	for _, attribute := range attributes {
		i.AddAttribute(attribute)
	}
}

func (i *Item) GetAttributes() []*Attribute {
	return i.attributes
}

func (i *Item) GetSize() int {
	size := 0
	for _, attribute := range i.attributes {
		size += len(attribute.Value)
	}

	return size
}

func NewBatchWriteItemRequest() *BatchWriteItemRequest {
	return &BatchWriteItemRequest{make(map[string]*BatchWriteItemOperations), false, false}
}

func (b *BatchWriteItemRequest) SetReturnConsumedCapacity(value bool) {
	b.returnConsumedCapacity = value
	log.Print("Parsing of the ReturnConsumedCapacity in the response not implemented")
}

func (b *BatchWriteItemRequest) SetReturnItemCollecionMetrics(value bool) {
	b.returnItemCollecionMetrics = value
	log.Print("Parsing of the ReturnItemCollecionMetrics in the response not implemented")
}

func (b *BatchWriteItemRequest) GetReturnConsumedCapacity() bool {
	return b.returnConsumedCapacity
}

func (b *BatchWriteItemRequest) GetReturnItemCollecionMetrics() bool {
	return b.returnItemCollecionMetrics
}

func (b *BatchWriteItemRequest) AddDeleteRequest(table string, item *Item) {
	if _, ok := b.operations[table]; !ok {
		b.operations[table] = &BatchWriteItemOperations{}
	}

	b.operations[table].deleteRequest = append(b.operations[table].deleteRequest, item)
}

func (b *BatchWriteItemRequest) AddPutRequest(table string, item *Item) {
	if _, ok := b.operations[table]; !ok {
		b.operations[table] = &BatchWriteItemOperations{}
	}

	b.operations[table].putRequest = append(b.operations[table].putRequest, item)
}

func (b *BatchWriteItemRequest) GetOperations() map[string]*BatchWriteItemOperations {
	return b.operations
}

func (b *BatchWriteItemRequest) GetItems() []*Item {
	items := []*Item{}
	for _, operations := range b.GetOperations() {

		for _, request := range operations.GetDeleteRequest() {
			items = append(items, request)
		}

		for _, request := range operations.GetPutRequest() {
			items = append(items, request)
		}
	}

	return items
}

func (b *BatchWriteItemOperations) GetDeleteRequest() []*Item {
	return b.deleteRequest
}

func (b *BatchWriteItemOperations) GetPutRequest() []*Item {
	return b.putRequest
}

func (t *Table) BatchGetItems(keys []Key) *BatchGetItem {
	batchGetItem := &BatchGetItem{t.Server, make(map[*Table][]Key)}

	batchGetItem.Keys[t] = keys
	return batchGetItem
}

func (batchGetItem *BatchGetItem) AddTable(t *Table, keys *[]Key) *BatchGetItem {
	batchGetItem.Keys[t] = *keys
	return batchGetItem
}

func (batchGetItem *BatchGetItem) Execute() (map[string][]map[string]*Attribute, error) {
	q := NewEmptyQuery()
	q.AddRequestItems(batchGetItem.Keys)

	jsonResponse, err := batchGetItem.Server.queryServer(target("BatchGetItem"), q)
	if err != nil {
		return nil, err
	}

	json, err := simplejson.NewJson(jsonResponse)

	if err != nil {
		return nil, err
	}

	results := make(map[string][]map[string]*Attribute)

	tables, err := json.Get("Responses").Map()
	if err != nil {
		message := fmt.Sprintf("Unexpected response %s", jsonResponse)
		return nil, errors.New(message)
	}

	for table, entries := range tables {
		var tableResult []map[string]*Attribute

		jsonEntriesArray, ok := entries.([]interface{})
		if !ok {
			message := fmt.Sprintf("Unexpected response %s", jsonResponse)
			return nil, errors.New(message)
		}

		for _, entry := range jsonEntriesArray {
			item, ok := entry.(map[string]interface{})
			if !ok {
				message := fmt.Sprintf("Unexpected response %s", jsonResponse)
				return nil, errors.New(message)
			}

			unmarshalledItem := parseAttributes(item)
			tableResult = append(tableResult, unmarshalledItem)
		}

		results[table] = tableResult
	}

	return results, nil
}

func (t *Table) GetItem(key *Key) (map[string]*Attribute, error) {
	q := NewQuery(t)
	q.AddKey(t, key)

	jsonResponse, err := t.Server.queryServer(target("GetItem"), q)
	if err != nil {
		return nil, err
	}

	json, err := simplejson.NewJson(jsonResponse)
	if err != nil {
		return nil, err
	}

	itemJson, ok := json.CheckGet("Item")
	if !ok {
		// We got an empty from amz. The item doesn't exist.
		return nil, ErrNotFound
	}

	item, err := itemJson.Map()
	if err != nil {
		message := fmt.Sprintf("Unexpected response %s", jsonResponse)
		return nil, errors.New(message)
	}

	return parseAttributes(item), nil

}

func (t *Table) BatchWriteItem(request *BatchWriteItemRequest) (map[string]map[string][]*Item, error) {
	if len(request.GetItems()) > 25 {
		return nil, errors.New("Each request cannot contain more than 25 items")
	}

	if len(request.GetItems()) == 0 {
		return nil, errors.New("The request must contain at least 1 item")
	}

	totalSize := 0
	for _, item := range request.GetItems() {
		size := item.GetSize()
		totalSize += size
		if size > 65536 {
			return nil, errors.New("The size of the item cannot exceed 64KB")
		}
	}

	if totalSize > 1048576 {
		return nil, errors.New("The size of the request cannot exceed 1MB")
	}

	q := NewEmptyQuery()
	q.AddBatchWriteItemOperations(request)

	jsonResponse, err := t.Server.queryServer(target("BatchWriteItem"), q)

	if err != nil {
		return nil, err
	}

	json, err := simplejson.NewJson(jsonResponse)

	if err != nil {
		return nil, err
	}

	results := make(map[string]map[string][]*Item)
	tables, err := json.Get("UnprocessedItems").Map()
	if err != nil {
		message := fmt.Sprintf("Unexpected response %s", jsonResponse)
		return nil, errors.New(message)
	}

	for table, containerJson := range tables {
		tableResult := make(map[string][]*Item)

		containerArray, ok := containerJson.([]interface{})
		if !ok {
			message := fmt.Sprintf("Unexpected response %s", containerArray)
			return nil, errors.New(message)
		}

		for _, container := range containerArray {
			operations, ok := container.(map[string]interface{})
			if !ok {
				message := fmt.Sprintf("Unexpected response %s", container)
				return nil, errors.New(message)
			}

			for opType, itemsJson := range operations {
				itemsArray, ok := itemsJson.(map[string]interface{})
				if !ok {
					message := fmt.Sprintf("Unexpected response %s", itemsJson)
					return nil, errors.New(message)
				}

				for _, attributesJson := range itemsArray {
					attributesArray, ok := attributesJson.(map[string]interface{})
					if !ok {
						message := fmt.Sprintf("Unexpected response %s", attributesJson)
						return nil, errors.New(message)
					}

					item := NewItem()
					item.AddAttributesFromMap(parseAttributes(attributesArray))

					tableResult[opType] = append(tableResult[opType], item)
				}
			}
		}

		results[table] = tableResult
	}

	return results, nil
}

func (t *Table) PutItem(item *Item) (bool, error) {

	if len(item.GetAttributes()) == 0 {
		return false, errors.New("At least one attribute is required.")
	}

	q := NewQuery(t)

	q.AddItem(item)

	jsonResponse, err := t.Server.queryServer(target("PutItem"), q)
	if err != nil {
		return false, err
	}

	_, err = simplejson.NewJson(jsonResponse)
	if err != nil {
		return false, err
	}

	return true, nil
}

func (t *Table) DeleteItem(key *Key) (bool, error) {

	q := NewQuery(t)
	q.AddKey(t, key)

	jsonResponse, err := t.Server.queryServer(target("DeleteItem"), q)

	if err != nil {
		return false, err
	}

	_, err = simplejson.NewJson(jsonResponse)
	if err != nil {
		return false, err
	}

	return true, nil
}

func (t *Table) AddAttributes(key *Key, attributes []Attribute) (bool, error) {
	return t.modifyAttributes(key, attributes, "ADD")
}

func (t *Table) UpdateAttributes(key *Key, attributes []Attribute) (bool, error) {
	return t.modifyAttributes(key, attributes, "PUT")
}

func (t *Table) DeleteAttributes(key *Key, attributes []Attribute) (bool, error) {
	return t.modifyAttributes(key, attributes, "DELETE")
}

func (t *Table) modifyAttributes(key *Key, attributes []Attribute, action string) (bool, error) {

	if len(attributes) == 0 {
		return false, errors.New("At least one attribute is required.")
	}

	q := NewQuery(t)
	q.AddKey(t, key)
	q.AddUpdates(attributes, action)

	jsonResponse, err := t.Server.queryServer(target("UpdateItem"), q)

	if err != nil {
		return false, err
	}

	_, err = simplejson.NewJson(jsonResponse)
	if err != nil {
		return false, err
	}

	return true, nil
}

func parseAttributes(s map[string]interface{}) map[string]*Attribute {
	results := map[string]*Attribute{}

	for key, value := range s {
		if v, ok := value.(map[string]interface{}); ok {
			if val, ok := v[TYPE_STRING].(string); ok {
				results[key] = &Attribute{
					Type:  TYPE_STRING,
					Name:  key,
					Value: val,
				}
			} else if val, ok := v[TYPE_NUMBER].(string); ok {
				results[key] = &Attribute{
					Type:  TYPE_NUMBER,
					Name:  key,
					Value: val,
				}
			} else if val, ok := v[TYPE_BINARY].(string); ok {
				results[key] = &Attribute{
					Type:  TYPE_BINARY,
					Name:  key,
					Value: val,
				}
			} else if vals, ok := v[TYPE_STRING_SET].([]interface{}); ok {
				arry := make([]string, len(vals))
				for i, ivalue := range vals {
					if val, ok := ivalue.(string); ok {
						arry[i] = val
					}
				}
				results[key] = &Attribute{
					Type:      TYPE_STRING_SET,
					Name:      key,
					SetValues: arry,
				}
			} else if vals, ok := v[TYPE_NUMBER_SET].([]interface{}); ok {
				arry := make([]string, len(vals))
				for i, ivalue := range vals {
					if val, ok := ivalue.(string); ok {
						arry[i] = val
					}
				}
				results[key] = &Attribute{
					Type:      TYPE_NUMBER_SET,
					Name:      key,
					SetValues: arry,
				}
			} else if vals, ok := v[TYPE_BINARY_SET].([]interface{}); ok {
				arry := make([]string, len(vals))
				for i, ivalue := range vals {
					if val, ok := ivalue.(string); ok {
						arry[i] = val
					}
				}
				results[key] = &Attribute{
					Type:      TYPE_BINARY_SET,
					Name:      key,
					SetValues: arry,
				}
			}
		} else {
			log.Printf("type assertion to map[string] interface{} failed for : %s\n ", value)
		}

	}

	return results
}
