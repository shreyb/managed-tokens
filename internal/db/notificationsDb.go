// COPYRIGHT 2024 FERMI NATIONAL ACCELERATOR LABORATORY
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
//
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package db

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
	"go.opentelemetry.io/otel/attribute"

	"github.com/fermitools/managed-tokens/internal/tracing"
)

// SQL statements to be used by API

// Query db actions
var (
	getSetupErrorsCountsStatement = `
	SELECT
		services.name,
		setup_errors.count
	FROM
		setup_errors
		INNER JOIN services ON services.id = setup_errors.service_id
	;
	`
	getSetupErrorsCountsByServiceStatement = `
	SELECT
		services.name,
		setup_errors.count
	FROM
		setup_errors
		INNER JOIN services ON services.id = setup_errors.service_id
	WHERE
		services.name = ?
	;
	`
	getPushErrorsCountsStatement = `
	SELECT
		services.name,
		nodes.name,
		push_errors.count
	FROM
		push_errors
		INNER JOIN services ON services.id = push_errors.service_id
		INNER JOIN nodes on nodes.id = push_errors.node_id
	;
	`
	getPushErrorsCountsByServiceStatement = `
	SELECT
		services.name,
		nodes.name,
		push_errors.count
	FROM
		push_errors
		INNER JOIN services ON services.id = push_errors.service_id
		INNER JOIN nodes on nodes.id = push_errors.node_id
	WHERE
		services.name = ?
	;
	`
	getAllServicesFromTableStatement = `
	SELECT name FROM services;
	`
	getAllNodesFromTableStatement = `
	SELECT name FROM nodes;
	`
)

// INSERT/UPDATE actions
var (
	insertIntoServicesTableStatement = `
	INSERT INTO services(name)
	VALUES
		(?)
	ON CONFLICT(name) DO NOTHING;
	`
	insertIntoNodesTableStatement = `
	INSERT INTO nodes(name)
	VALUES
		(?)
	ON CONFLICT(name) DO NOTHING;
	`
	insertOrUpdateSetupErrorsStatement = `
	INSERT INTO setup_errors(service_id, count)
	SELECT
		(SELECT services.id FROM services WHERE services.name = ?) AS service_id,
		? AS count
	ON CONFLICT(service_id) DO
		UPDATE SET count = ?
	;
	`
	insertOrUpdatePushErrorsStatement = `
	INSERT INTO push_errors(service_id, node_id, count)
	SELECT
		(SELECT services.id FROM services WHERE services.name = ?) AS service_id,
		(SELECT nodes.id FROM nodes WHERE nodes.name = ?) AS node_id,
		? as count
	ON CONFLICT(service_id, node_id) DO
		UPDATE SET count = ?
	;
	`
)

// GetAllServices queries the ManagedTokensDatabase for the registered services
// and returns a slice of strings with their names
func (m *ManagedTokensDatabase) GetAllServices(ctx context.Context) ([]string, error) {
	ctx, span := tracer.Start(ctx, "ManagedTokensDatabase.GetAllServices")
	defer span.End()

	dataConverted, err := getNamedDimensionStringValues(ctx, m.db, getAllServicesFromTableStatement)
	if err != nil {
		err := fmt.Errorf("could not get service names from database: %w", err)
		tracing.LogErrorWithTrace(
			span,
			err,
			tracing.KeyValueForLog{Key: "dbLocation", Value: m.filename},
		)
		return nil, err
	}
	tracing.LogSuccessWithTrace(span, "Got service names from database")
	return dataConverted, nil
}

// GetAllNodes queries the ManagedTokensDatabase for the registered nodes
// and returns a slice of strings with their names
func (m *ManagedTokensDatabase) GetAllNodes(ctx context.Context) ([]string, error) {
	ctx, span := tracer.Start(ctx, "ManagedTokensDatabase.GetAllNodes")
	defer span.End()

	dataConverted, err := getNamedDimensionStringValues(ctx, m.db, getAllNodesFromTableStatement)
	if err != nil {
		err := fmt.Errorf("could not get node names from database: %w", err)
		tracing.LogErrorWithTrace(
			span,
			err,
			tracing.KeyValueForLog{Key: "dbLocation", Value: m.filename},
		)
		return nil, err
	}
	tracing.LogSuccessWithTrace(span, "Got node names from database")
	return dataConverted, nil
}

// Setup Errors

// SetupErrorCount is an interface that wraps the Service and Count methods.  It is meant to be used both by this package and importing packages to
// retrieve service and count information about setupErrors.
type SetupErrorCount interface {
	Service() string
	Count() int
}

// setupErrorCount is an internal-facing type that implements both SetupErrorCount and insertData
type setupErrorCount struct {
	service string
	count   int
}

func (s *setupErrorCount) Service() string { return s.service }
func (s *setupErrorCount) Count() int      { return s.count }

// s.count is doubled here because of the ON CONFLICT...UPDATE clause
func (s *setupErrorCount) insertValues() []any { return []any{s.service, s.count, s.count} }

func (s *setupErrorCount) unpackDataRow(resultRow []any) (dataRowUnpacker, error) {
	// Make sure we have the right number of values
	if len(resultRow) != 2 {
		return nil, &errDatabaseDataWrongStructure{resultRow, "expected row with 2 values"}
	}
	// Type check each element
	serviceVal, serviceTypeOk := resultRow[0].(string)
	countVal, countTypeOk := resultRow[1].(int64)
	if !(serviceTypeOk && countTypeOk) {
		_valsForErr := []any{resultRow[0], resultRow[1]}
		return nil, &errDatabaseDataWrongType{_valsForErr, "expected (string, int64)"}
	}
	return &setupErrorCount{serviceVal, int(countVal)}, nil
}

// GetSetupErrorsInfo queries the ManagedTokensDatabase for setup error counts.  It returns the data in the form of a slice of SetupErrorCounts
// that the caller can unpack using the interface methods Service() and Count()
func (m *ManagedTokensDatabase) GetSetupErrorsInfo(ctx context.Context) ([]SetupErrorCount, error) {
	ctx, span := tracer.Start(ctx, "ManagedTokensDatabase.GetSetupErrorsInfo")
	span.SetAttributes(attribute.String("dbLocation", m.filename))
	defer span.End()

	data, err := getValuesTransactionRunner(ctx, m.db, getSetupErrorsCountsStatement)
	if err != nil {
		err = fmt.Errorf("could not get setup errors information from database: %w", err)
		tracing.LogErrorWithTrace(span, err)
		return nil, err
	}

	if len(data) == 0 {
		span.AddEvent("No setup error data in database")
		return nil, sql.ErrNoRows
	}

	// Unpack data
	unpackedData, err := unpackData[*setupErrorCount](data)
	if err != nil {
		err = fmt.Errorf("error unpacking setupErrorCount data: %w", err)
		tracing.LogErrorWithTrace(span, err)
		return nil, err
	}
	convertedData := make([]SetupErrorCount, 0, len(unpackedData))
	for _, datum := range unpackedData {
		convertedData = append(convertedData, datum)
	}

	tracing.LogSuccessWithTrace(span, "Got setup errors information from ManagedTokensDatabase")
	return convertedData, nil
}

// GetSetupErrorsInfoByService queries the ManagedTokensDatabase for the setup errors for a specific service.  It returns the data as a SetupErrorCount that
// calling functions can unpack using the Service() or Count() functions.
func (m *ManagedTokensDatabase) GetSetupErrorsInfoByService(ctx context.Context, service string) (SetupErrorCount, error) {
	ctx, span := tracer.Start(ctx, "ManagedTokensDatabase.GetSetupErrorsInfoByService")
	span.SetAttributes(attribute.String("service", service), attribute.String("dbLocation", m.filename))
	defer span.End()

	data, err := getValuesTransactionRunner(ctx, m.db, getSetupErrorsCountsByServiceStatement, service)
	if err != nil {
		err = fmt.Errorf("could not get setup errors information from database: %w", err)
		tracing.LogErrorWithTrace(span, err)
		return nil, err
	}

	if len(data) == 0 {
		span.AddEvent("No setup error data in database")
		return nil, sql.ErrNoRows
	}

	if len(data) != 1 {
		err := &errDatabaseDataWrongStructure{data, "setup errors query result has wrong structure.  Expected row with 1 value"}
		tracing.LogErrorWithTrace(span, err)
		return nil, err
	}

	unpackedData, err := unpackData[*setupErrorCount](data)
	if err != nil {
		err = fmt.Errorf("error unpacking setupErrorCount data: %w", err)
		tracing.LogErrorWithTrace(span, err)
		return nil, err
	}
	tracing.LogSuccessWithTrace(span, "Got setup errors information from ManagedTokensDatabase")
	return unpackedData[0], nil
}

// UpdateSetupErrorsTable updates the setup errors table of the ManagedTokens database.  The information to be modified
// in the database should be given as a slice of SetupErrorCount (setupErrorsByService)
func (m *ManagedTokensDatabase) UpdateSetupErrorsTable(ctx context.Context, setupErrorsByService []SetupErrorCount) error {
	ctx, span := tracer.Start(ctx, "ManagedTokensDatabase.UpdateSetupErrorsTable")
	span.SetAttributes(attribute.String("dbLocation", m.filename))
	defer span.End()

	setupErrorDatumSlice := setupErrorCountInterfaceSliceToInsertValuesSlice(setupErrorsByService)

	if err := insertValuesTransactionRunner(ctx, m.db, insertOrUpdateSetupErrorsStatement, setupErrorDatumSlice); err != nil {
		err = fmt.Errorf("could not update setup errors in database: %w", err)
		tracing.LogErrorWithTrace(span, err)
		return err
	}
	tracing.LogSuccessWithTrace(span, "Updated setup errors in ManagedTokensDatabase")
	return nil
}

// setupErrorCountInterfaceSliceToInsertValuesSlice converts a []SetupErrorCount to []insertValues
func setupErrorCountInterfaceSliceToInsertValuesSlice(setupErrorCountInterfaceSlice []SetupErrorCount) []insertValues {
	sl := make([]insertValues, 0, len(setupErrorCountInterfaceSlice))
	for _, datum := range setupErrorCountInterfaceSlice {
		sl = append(sl,
			&setupErrorCount{
				service: datum.Service(),
				count:   datum.Count(),
			})
	}
	return sl
}

// Push Errors

// PushErrorCount is an interface that wraps the Service, Node, and Count methods.  It is meant to be used both by this package and
// importing packages to retrieve service, node, and count information about pushErrors.
type PushErrorCount interface {
	Service() string
	Node() string
	Count() int
}

// pushErrorCount is an internal-facing type that implements both PushErrorCount and insertValues
type pushErrorCount struct {
	service string
	node    string
	count   int
}

func (p *pushErrorCount) Service() string { return p.service }
func (p *pushErrorCount) Node() string    { return p.node }
func (p *pushErrorCount) Count() int      { return p.count }

// p.count is doubled here because of the ON CONFLICT...UPDATE clause
func (p *pushErrorCount) insertValues() []any { return []any{p.service, p.node, p.count, p.count} }

func (p *pushErrorCount) unpackDataRow(resultRow []any) (dataRowUnpacker, error) {
	// Make sure we have the right number of values
	if len(resultRow) != 3 {
		return nil, &errDatabaseDataWrongStructure{resultRow, "push error data has wrong structure"}
	}
	// Type check each element
	serviceVal, serviceTypeOk := resultRow[0].(string)
	nodeVal, nodeTypeOk := resultRow[1].(string)
	countVal, countTypeOk := resultRow[2].(int64)
	if !(serviceTypeOk && nodeTypeOk && countTypeOk) {
		return nil, &errDatabaseDataWrongType{resultRow, "expected (string, string, int64)"}
	}
	return &pushErrorCount{serviceVal, nodeVal, int(countVal)}, nil
}

// GetPushErrorsInfo queries the ManagedTokensDatabase for push error counts.  It returns the data in the form of a slice of PushErrorCounts
// that the caller can unpack using the interface methods Service(), Node(), and Count()
func (m *ManagedTokensDatabase) GetPushErrorsInfo(ctx context.Context) ([]PushErrorCount, error) {
	ctx, span := tracer.Start(ctx, "ManagedTokensDatabase.GetPushErrorsInfo")
	span.SetAttributes(attribute.String("dbLocation", m.filename))
	defer span.End()

	data, err := getValuesTransactionRunner(ctx, m.db, getPushErrorsCountsStatement)
	if err != nil {
		err = fmt.Errorf("could not get push errors information from database: %w", err)
		tracing.LogErrorWithTrace(span, err)
		return nil, err
	}

	if len(data) == 0 {
		span.AddEvent("No push error data in database")
		return nil, sql.ErrNoRows
	}

	// Unpack data
	unpackedData, err := unpackData[*pushErrorCount](data)
	if err != nil {
		err = fmt.Errorf("error unpacking pushErrorCount data: %w", err)
		tracing.LogErrorWithTrace(span, err)
		return nil, err
	}
	convertedData := make([]PushErrorCount, 0, len(unpackedData))
	for _, datum := range unpackedData {
		convertedData = append(convertedData, datum)
	}
	tracing.LogSuccessWithTrace(span, "Got push errors information from ManagedTokensDatabase")
	return convertedData, nil
}

// GetPushErrorsInfoByService queries the database for the push errors for a specific service.  It returns the data as a slice of PushErrorCounts
// that the caller can unpack using the Service(), Node(), and Count() interface methods.
func (m *ManagedTokensDatabase) GetPushErrorsInfoByService(ctx context.Context, service string) ([]PushErrorCount, error) {
	ctx, span := tracer.Start(ctx, "ManagedTokensDatabase.GetPushErrorsInfoByService")
	span.SetAttributes(
		attribute.String("service", service),
		attribute.String("dbLocation", m.filename),
	)
	defer span.End()

	data, err := getValuesTransactionRunner(ctx, m.db, getPushErrorsCountsByServiceStatement, service)
	if err != nil {
		err = fmt.Errorf("could not get push errors information from database: %w", err)
		tracing.LogErrorWithTrace(span, err)
		return nil, err
	}

	if len(data) == 0 {
		span.AddEvent("No push error data in database")
		return nil, sql.ErrNoRows
	}

	// Unpack data
	unpackedData, err := unpackData[*pushErrorCount](data)
	if err != nil {
		err = fmt.Errorf("error unpacking pushErrorCount data: %w", err)
		tracing.LogErrorWithTrace(span, err)
		return nil, err
	}
	convertedData := make([]PushErrorCount, 0, len(unpackedData))
	for _, datum := range unpackedData {
		convertedData = append(convertedData, datum)
	}
	tracing.LogSuccessWithTrace(span, "Got push errors information from ManagedTokensDatabase")
	return convertedData, nil
}

// UpdatePushErrorsTable updates the push errors table of the ManagedTokens database.  The information to be modified
// in the database should be given as a slice of PushErrorCount (pushErrorsByServiceAndNode)
func (m *ManagedTokensDatabase) UpdatePushErrorsTable(ctx context.Context, pushErrorsByServiceAndNode []PushErrorCount) error {
	ctx, span := tracer.Start(ctx, "ManagedTokensDatabase.UpdatePushErrorsTable")
	span.SetAttributes(attribute.String("dbLocation", m.filename))
	defer span.End()

	pushErrorDatumSlice := pushErrorCountInterfaceSliceToInsertValuesSlice(pushErrorsByServiceAndNode)

	if err := insertValuesTransactionRunner(ctx, m.db, insertOrUpdatePushErrorsStatement, pushErrorDatumSlice); err != nil {
		err = fmt.Errorf("could not update push errors in database: %w", err)
		tracing.LogErrorWithTrace(span, err)
		return err
	}
	tracing.LogSuccessWithTrace(span, "Updated push errors in ManagedTokensDatabase")
	return nil
}

// pushErrorCountInterfaceSliceToInsertValuesSlice converts a []PushErrorCount to []insertValues
func pushErrorCountInterfaceSliceToInsertValuesSlice(pushErrorCountInterfaceSlice []PushErrorCount) []insertValues {
	sl := make([]insertValues, 0, len(pushErrorCountInterfaceSlice))
	for _, datum := range pushErrorCountInterfaceSlice {
		sl = append(sl,
			&pushErrorCount{
				service: datum.Service(),
				node:    datum.Node(),
				count:   datum.Count(),
			})
	}
	return sl
}

// Dimension data

// serviceDatum is an internal type that implements the insertValues interface.  It holds the name of a service as its value.
type serviceDatum string

func (s *serviceDatum) insertValues() []any { return []any{s} }

// UpdateServices updates the services table in the ManagedTokensDatabase.  It takes a slice
// of strings for the service names, and inserts them if they don't already exist in the
// database
func (m *ManagedTokensDatabase) UpdateServices(ctx context.Context, serviceNames []string) error {
	ctx, span := tracer.Start(ctx, "ManagedTokensDatabase.UpdateServices")
	span.SetAttributes(
		attribute.String("dbLocation", m.filename),
		attribute.StringSlice("serviceNames", serviceNames),
	)
	defer span.End()

	serviceDatumSlice := convertStringSliceToInsertValuesSlice(
		newInsertValuesFromUnderlyingString[*serviceDatum, serviceDatum],
		serviceNames,
	)

	if err := insertValuesTransactionRunner(ctx, m.db, insertIntoServicesTableStatement, serviceDatumSlice); err != nil {
		err = fmt.Errorf("could not update services in database: %w", err)
		tracing.LogErrorWithTrace(span, err)
		return err
	}

	tracing.LogSuccessWithTrace(span, "Updated services in ManagedTokensDatabase")
	return nil
}

// nodeDatum is an internal type that implements the insertValues interface.  It holds the name of a node as its value.
type nodeDatum string

func (n *nodeDatum) insertValues() []any { return []any{n} }

// UpdateNodes updates the nodes table in the ManagedTokensDatabase.  It takes a slice
// of strings for the node names, and inserts them if they don't already exist in the
// database
func (m *ManagedTokensDatabase) UpdateNodes(ctx context.Context, nodes []string) error {
	ctx, span := tracer.Start(ctx, "ManagedTokensDatabase.UpdateNodes")
	span.SetAttributes(
		attribute.String("dbLocation", m.filename),
		attribute.StringSlice("nodes", nodes),
	)
	defer span.End()

	nodesDatumSlice := convertStringSliceToInsertValuesSlice(
		newInsertValuesFromUnderlyingString[*nodeDatum, nodeDatum],
		nodes,
	)

	if err := insertValuesTransactionRunner(ctx, m.db, insertIntoNodesTableStatement, nodesDatumSlice); err != nil {
		err = fmt.Errorf("could not update nodes in database: %w", err)
		tracing.LogErrorWithTrace(span, err)
		return err
	}

	tracing.LogSuccessWithTrace(span, "Updated nodes in ManagedTokensDatabase")
	return nil

}
