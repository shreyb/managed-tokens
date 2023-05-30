package db

import (
	"context"
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"
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

// External-facing functions to modify db
// TODO Make sure all funcs are documented

// GetAllServices queries the NotificationsDatabase for the registered services
// and returns a slice of strings with their names
func (m *ManagedTokensDatabase) GetAllServices(ctx context.Context) ([]string, error) {
	dataConverted, err := m.getNamedDimensionStringValues(ctx, getAllServicesFromTableStatement)
	if err != nil {
		log.Error("Could not get service names from database")
		return nil, err
	}
	return dataConverted, nil
}

// GetAllNodes queries the NotificationsDatabase for the registered nodes
// and returns a slice of strings with their names
func (m *ManagedTokensDatabase) GetAllNodes(ctx context.Context) ([]string, error) {
	dataConverted, err := m.getNamedDimensionStringValues(ctx, getAllNodesFromTableStatement)
	if err != nil {
		log.Error("Could not get node names from database")
		return nil, err
	}
	return dataConverted, nil
}

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
func (s *setupErrorCount) values() []any   { return []any{s.service, s.count, s.count} }

// GetSetupErrorsInfo queries the ManagedTokensDatabase for setup error counts.  It returns the data in the form of a slice of SetupErrorCounts
// that the caller can unpack using the interface methods Service() and Count()
func (m *ManagedTokensDatabase) GetSetupErrorsInfo(ctx context.Context) ([]SetupErrorCount, error) {
	dataConverted := make([]SetupErrorCount, 0)
	data, err := getValuesTransactionRunner(ctx, m.db, getSetupErrorsCountsStatement)
	if err != nil {
		log.Error("Could not get setup errors information from ManagedTokensDatabase")
		return dataConverted, err
	}

	if len(data) == 0 {
		log.Debug("No setup error data in database")
		return nil, sql.ErrNoRows
	}

	// Unpack data
	for _, resultRow := range data {
		// Make sure we have the right number of values
		if len(resultRow) != 2 {
			msg := "setup error data has wrong structure"
			log.Errorf("%s: %v", msg, resultRow)
			return dataConverted, errDatabaseDataWrongStructure
		}
		// Type check each element
		serviceVal, serviceTypeOk := resultRow[0].(string)
		countVal, countTypeOk := resultRow[1].(int64)
		if !(serviceTypeOk && countTypeOk) {
			msg := "setup errors query result has wrong type.  Expected (int, string)"
			log.Errorf("%s: got (%T, %T)", msg, serviceVal, countVal)
			return dataConverted, errDatabaseDataWrongType
		}
		log.Debugf("Got SetupError row: %s, %d", serviceVal, countVal)
		dataConverted = append(dataConverted, &setupErrorCount{serviceVal, int(countVal)})
	}
	return dataConverted, nil
}

// GetSetupErrorsInfoByService queries the database for the setup errors for a specific service.  It returns the data as a SetupErrorCount that
// calling functions can unpack using the Service() or Count() functions.
func (m *ManagedTokensDatabase) GetSetupErrorsInfoByService(ctx context.Context, service string) (SetupErrorCount, error) {
	data, err := getValuesTransactionRunner(ctx, m.db, getSetupErrorsCountsByServiceStatement, service)
	if err != nil {
		log.Error("Could not get setup errors information from ManagedTokensDatabase")
		return nil, err
	}

	if len(data) == 0 {
		log.Debug("No setup error data in database")
		return nil, sql.ErrNoRows
	}

	if len(data) != 1 {
		msg := "setup error data should only have 1 row"
		log.Errorf("%s: %v", msg, data)
		return nil, errDatabaseDataWrongStructure
	}

	resultRow := data[0]
	if len(resultRow) != 2 {
		msg := "setup error data has wrong structure"
		log.Errorf("%s: %v", msg, resultRow)
		return nil, errDatabaseDataWrongStructure
	}
	// Type check each element
	serviceVal, serviceTypeOk := resultRow[0].(string)
	countVal, countTypeOk := resultRow[1].(int64)
	if !(serviceTypeOk && countTypeOk) {
		msg := "setup errors query result has wrong type.  Expected (int, string)"
		log.Errorf("%s: got (%T, %T)", msg, serviceVal, countVal)
		return nil, errDatabaseDataWrongType
	}
	log.Debugf("Got SetupError row: %s, %d", serviceVal, countVal)
	return &setupErrorCount{serviceVal, int(countVal)}, nil
}

// PushErrorCount is an interface that wraps the Service, Node, and Count methods.  It is meant to be used both by this package and
// importing packages to retrieve service, node, and count information about pushErrors.
type PushErrorCount interface {
	Service() string
	Node() string
	Count() int
}

// pushErrorCount is an internal-facing type that implements both PushErrorCount and insertData
type pushErrorCount struct {
	service string
	node    string
	count   int
}

func (p *pushErrorCount) Service() string { return p.service }
func (p *pushErrorCount) Node() string    { return p.node }
func (p *pushErrorCount) Count() int      { return p.count }
func (p *pushErrorCount) values() []any   { return []any{p.service, p.node, p.count, p.count} }

// GetPushErrorsInfo queries the ManagedTokensDatabase for push error counts.  It returns the data in the form of a slice of PushErrorCounts
// that the caller can unpack using the interface methods Service(), Node(), and Count()
func (m *ManagedTokensDatabase) GetPushErrorsInfo(ctx context.Context) ([]PushErrorCount, error) {
	dataConverted := make([]PushErrorCount, 0)
	data, err := getValuesTransactionRunner(ctx, m.db, getPushErrorsCountsStatement)
	if err != nil {
		log.Error("Could not get push errors information from ManagedTokensDatabase")
		return dataConverted, err
	}

	if len(data) == 0 {
		log.Debug("No push error data in database")
		return dataConverted, sql.ErrNoRows
	}

	// Unpack data
	for _, resultRow := range data {
		if len(resultRow) != 3 {
			msg := "push error data has wrong structure"
			log.Errorf("%s: %v", msg, resultRow)
			return dataConverted, errDatabaseDataWrongStructure
		}
		// Type check each element
		serviceVal, serviceTypeOk := resultRow[0].(string)
		nodeVal, nodeTypeOk := resultRow[1].(string)
		countVal, countTypeOk := resultRow[2].(int64)
		if !(serviceTypeOk && nodeTypeOk && countTypeOk) {
			msg := "push errors query result has wrong type.  Expected (string, string, int)"
			log.Errorf("%s: got (%T, %T, %T)", msg, serviceVal, nodeVal, countVal)
			return dataConverted, errDatabaseDataWrongType
		}
		log.Debugf("Got PushErrorCount row: %s, %s, %d", serviceVal, nodeVal, countVal)
		dataConverted = append(dataConverted, &pushErrorCount{serviceVal, nodeVal, int(countVal)})
	}
	return dataConverted, nil
}

// GetPushErrorsInfoByService queries the database for the push errors for a specific service.  It returns the data as a slice of PushErrorCounts
// that the caller can unpack using the Service(), Node(), and Count() interface methods.
func (m *ManagedTokensDatabase) GetPushErrorsInfoByService(ctx context.Context, service string) ([]PushErrorCount, error) {
	dataConverted := make([]PushErrorCount, 0)
	data, err := getValuesTransactionRunner(ctx, m.db, getPushErrorsCountsByServiceStatement, service)
	if err != nil {
		log.Error("Could not get push errors information from ManagedTokensDatabase")
		return nil, err
	}

	if len(data) == 0 {
		log.Debug("No push error data in database")
		return nil, sql.ErrNoRows
	}

	// Unpack data
	for _, resultRow := range data {
		if len(resultRow) != 3 {
			msg := "push error data has wrong structure"
			log.Errorf("%s: %v", msg, resultRow)
			return dataConverted, errDatabaseDataWrongStructure
		}
		// Type check each element
		serviceVal, serviceTypeOk := resultRow[0].(string)
		nodeVal, nodeTypeOk := resultRow[1].(string)
		countVal, countTypeOk := resultRow[2].(int64)
		if !(serviceTypeOk && nodeTypeOk && countTypeOk) {
			msg := "push errors query result has wrong type.  Expected (string, string, int)"
			log.Errorf("%s: got (%T, %T, %T)", msg, serviceVal, nodeVal, countVal)
			return dataConverted, errDatabaseDataWrongType
		}
		log.Debugf("Got PushErrorCount row: %s, %s, %d", serviceVal, nodeVal, countVal)
		dataConverted = append(dataConverted, &pushErrorCount{serviceVal, nodeVal, int(countVal)})
	}
	return dataConverted, nil
}

// serviceDatum is an internal type that implements the insertValues interface.  It holds the name of a service as its value.
type serviceDatum string

func (s *serviceDatum) values() []any { return []any{s} }

// UpdateServices updates the services table in the NotificationsDatabase.  It takes a slice
// of strings for the service names, and inserts them if they don't already exist in the
// database
func (m *ManagedTokensDatabase) UpdateServices(ctx context.Context, serviceNames []string) error {
	serviceDatumSlice := make([]insertValues, 0, len(serviceNames))
	for _, s := range serviceNames {
		sDatum := serviceDatum(s)
		serviceDatumSlice = append(serviceDatumSlice, &sDatum)
	}

	if err := insertValuesTransactionRunner(ctx, m.db, insertIntoServicesTableStatement, serviceDatumSlice); err != nil {
		log.Error("Could not update services in ManagedTokensDatabase")
		return err
	}
	log.Debug("Updated services in ManagedTokensDatabase")
	return nil
}

// nodeDatum is an internal type that implements the insertValues interface.  It holds the name of a node as its value.
type nodeDatum string

func (n *nodeDatum) values() []any { return []any{n} }

// UpdateNodes updates the nodes table in the NotificationsDatabase.  It takes a slice
// of strings for the node names, and inserts them if they don't already exist in the
// database
func (m *ManagedTokensDatabase) UpdateNodes(ctx context.Context, nodes []string) error {
	nodesDatumSlice := make([]insertValues, 0, len(nodes))
	for _, n := range nodes {
		nDatum := nodeDatum(n)
		nodesDatumSlice = append(nodesDatumSlice, &nDatum)
	}

	if err := insertValuesTransactionRunner(ctx, m.db, insertIntoNodesTableStatement, nodesDatumSlice); err != nil {
		log.Error("Could not update nodes in ManagedTokensDatabase")
		return err
	}
	log.Debug("Updated nodes in ManagedTokensDatabase")
	return nil

}

// UpdateSetupErrorsTable updates the setup errors table of the ManagedTokens database.  The information to be modified
// in the database should be given as a slice of SetupErrorCount (setupErrorsByService)
func (m *ManagedTokensDatabase) UpdateSetupErrorsTable(ctx context.Context, setupErrorsByService []SetupErrorCount) error {
	setupErrorDatumSlice := make([]insertValues, 0, len(setupErrorsByService))
	for _, datum := range setupErrorsByService {
		setupErrorDatumSlice = append(setupErrorDatumSlice,
			&setupErrorCount{
				service: datum.Service(),
				count:   datum.Count(),
			})
	}

	if err := insertValuesTransactionRunner(ctx, m.db, insertOrUpdateSetupErrorsStatement, setupErrorDatumSlice); err != nil {
		log.Error("Could not update setup errors in ManagedTokensDatabase")
		return err
	}
	log.Debug("Updated setup errors in ManagedTokensDatabase")
	return nil

}

// UpdatePushErrorsTable updates the push errors table of the ManagedTokens database.  The information to be modified
// in the database should be given as a slice of PushErrorCount (pushErrorsByServiceAndNode)
func (m *ManagedTokensDatabase) UpdatePushErrorsTable(ctx context.Context, pushErrorsByServiceAndNode []PushErrorCount) error {
	pushErrorDatumSlice := make([]insertValues, 0, len(pushErrorsByServiceAndNode))
	for _, datum := range pushErrorsByServiceAndNode {
		pushErrorDatumSlice = append(pushErrorDatumSlice,
			&pushErrorCount{
				service: datum.Service(),
				node:    datum.Node(),
				count:   datum.Count(),
			})
	}

	if err := insertValuesTransactionRunner(ctx, m.db, insertOrUpdatePushErrorsStatement, pushErrorDatumSlice); err != nil {
		log.Error("Could not update push errors in ManagedTokensDatabase")
		return err
	}
	log.Debug("Updated push errors in ManagedTokensDatabase")
	return nil
}

// getNamedDimensionStringValues queries a table as given in the sqlGetStatement provided that each row returned by the query
// in sqlGetStatement is a single string (one column of string type). An example of a valid query for sqlGetStatement would be
// "SELECT name FROM table".  An invalid query would be "SELECT id, name FROM table"
func (m *ManagedTokensDatabase) getNamedDimensionStringValues(ctx context.Context, sqlGetStatement string) ([]string, error) {
	dataConverted := make([]string, 0)
	data, err := getValuesTransactionRunner(ctx, m.db, sqlGetStatement)
	if err != nil {
		log.Error("Could not get values from database")
		return dataConverted, err
	}

	if len(data) == 0 {
		log.Debug("No values in database")
		return dataConverted, nil
	}

	for _, resultRow := range data {
		if len(resultRow) != 1 {
			msg := "dimension name data has wrong structure"
			log.Errorf("%s: %v", msg, resultRow)
			return dataConverted, errDatabaseDataWrongStructure
		}
		if val, ok := resultRow[0].(string); !ok {
			msg := "dimension name query result has wrong type.  Expected string"
			log.Errorf("%s: got %T", msg, val)
			return dataConverted, errDatabaseDataWrongType
		} else {
			log.Debugf("Got dimension row: %s", val)
			dataConverted = append(dataConverted, val)
		}
	}
	return dataConverted, nil
}
