/*
 The MIT License

 Permission is hereby granted, free of charge, to any person obtaining a copy
 of this software and associated documentation files (the "Software"), to deal
 in the Software without restriction, including without limitation the rights
 to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 copies of the Software, and to permit persons to whom the Software is
 furnished to do so, subject to the following conditions:

 The above copyright notice and this permission notice shall be included in
 all copies or substantial portions of the Software.

 THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
 THE SOFTWARE.
*/

package influxdb3

import (
	"context"
	"crypto/x509"

	// "encoding/json"
	"fmt"

	"github.com/apache/arrow/go/v13/arrow"
	"github.com/apache/arrow/go/v13/arrow/flight"
	"github.com/apache/arrow/go/v13/arrow/flight/flightsql"
	"github.com/apache/arrow/go/v13/arrow/ipc"
	"github.com/apache/arrow/go/v13/arrow/memory"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

func (c *Client) initializeQueryClient() error {
	url, safe := ReplaceURLProtocolWithPort(c.config.Host)

	var transport grpc.DialOption

	if safe == nil || *safe {
		pool, err := x509.SystemCertPool()
		if err != nil {
			return fmt.Errorf("x509: %s", err)
		}
		transport = grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(pool, ""))
	} else {
		transport = grpc.WithTransportCredentials(insecure.NewCredentials())
	}

	opts := []grpc.DialOption{
		transport,
	}

	client, err := flight.NewClientWithMiddleware(url, nil, nil, opts...)
	if err != nil {
		return fmt.Errorf("flight: %s", err)
	}
	c.queryClient = &client
	clientFlight, err := flightsql.NewClient(url, nil, nil, opts...);
	if err != nil {
		return fmt.Errorf("flight: %s", err)
	}
	c.queryClientSql = clientFlight;


	return nil
}

// Query data from InfluxDB IOx with FlightSQL.
// Parameters:
//   - ctx: The context.Context to use for the request.
//   - query: The InfluxQL query string to execute.
//
// Returns:
//   - A custom iterator (*QueryIterator).
//   - An error, if any.
func (c *Client) Query(ctx context.Context, query string) (*QueryIterator, error) {
	return c.QueryWithOptions(ctx, &DefaultQueryOptions, query, nil)
}

// Query data from InfluxDB IOx with FlightSQL.
// Parameters:
//   - ctx: The context.Context to use for the request.
//   - query: The InfluxQL query string to execute.
//	 - params: The params to be placed inside query.
//
// Returns:
//   - A custom iterator (*QueryIterator).
//   - An error, if any.
func (c *Client) QueryParametrized(ctx context.Context, query string, params arrow.Record) (*QueryIterator, error) {
	return c.QueryWithOptions(ctx, &DefaultQueryOptions, query, params)
}

// Query data from InfluxDB IOx with query options.
// Parameters:
//   - ctx: The context.Context to use for the request.
//   - options: Query options (query type, optional database).
//   - query: The query string to execute.
//	 - params: The params to be placed inside query.
//
// Returns:
//   - A custom iterator (*QueryIterator) that can also be used to get raw flightsql reader.
//   - An error, if any.
func (c *Client) QueryWithOptions(ctx context.Context, options *QueryOptions, query string, params arrow.Record) (*QueryIterator, error) {
	if options == nil {
		return nil, fmt.Errorf("options not set")
	}

	var database string
	// var queryType QueryType
	if options.Database != "" {
		database = options.Database
	} else {
		database = c.config.Database
	}
	if database == "" {
		return nil, fmt.Errorf("database not specified")
	}
	// queryType = options.QueryType

	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+c.config.Token)
	ctx = metadata.AppendToOutgoingContext(ctx, "database", database)

	// ticketData := map[string]interface{}{
	// 	"database":   database,
	// 	"sql_query":  query,
	// 	"query_type": strings.ToLower(queryType.String()),
	// }

	// ticketJson, err := json.Marshal(ticketData)
	// if err != nil {
	// 	return nil, fmt.Errorf("serialize: %s", err)
	// }

	stmt, err := c.queryClientSql.Prepare(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("flight prepare: %s", err)
	}
	defer stmt.Close(ctx)

	if (params != nil) {
		stmt.SetParameters(params);
	}
	info, err := stmt.Execute(ctx);
	if err != nil {
		return nil, fmt.Errorf("flight prepare: %s", err)
	}

	tick := info.Endpoint[0].Ticket.String();
	print(tick);

	stream, err := (*c.queryClient).DoGet(ctx, info.Endpoint[0].Ticket)
	if err != nil {
		return nil, fmt.Errorf("flight doget: %s", err)
	}
	reader, err := flight.NewRecordReader(stream, ipc.WithAllocator(memory.DefaultAllocator))
	if err != nil {
		return nil, fmt.Errorf("flight reader: %s", err)
	}

	iterator := newQueryIterator(reader)
	return iterator, nil
}
