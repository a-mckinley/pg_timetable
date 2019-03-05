package pgengine_test

import (
	"fmt"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/cybertec-postgresql/pg_timetable/internal/tasks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestDBFunc used to conect and to initialize test PostgreSQL database
var setupTestDBFunc = func() {
	pgengine.InitAndTestConfigDBConnection("localhost", "5432", "timetable", "scheduler",
		"scheduler", "disable", pgengine.SQLSchemaFiles)
}

func setupTestCase(t *testing.T) func(t *testing.T) {
	pgengine.ClientName = "pgengine_unit_test"
	t.Log("Setup test case")
	setupTestDBFunc()
	return func(t *testing.T) {
		pgengine.ConfigDb.MustExec("DROP SCHEMA IF EXISTS timetable CASCADE")
		t.Log("Test schema dropped")
	}
}

func TestBootstrapSQLFileExists(t *testing.T) {
	for _, f := range pgengine.SQLSchemaFiles {
		assert.FileExists(t, f, "Bootstrap file doesn't exist")
	}
}

func TestCreateConfigDBSchemaWithoutFile(t *testing.T) {
	assert.Panics(t, func() { pgengine.CreateConfigDBSchema("wrong path") }, "Should panic with nonexistent file")
}

func TestInitAndTestConfigDBConnection(t *testing.T) {
	teardownTestCase := setupTestCase(t)
	defer teardownTestCase(t)

	require.NotNil(t, pgengine.ConfigDb, "ConfigDB should be initialized")

	t.Run("Check timetable tables", func(t *testing.T) {
		var oid int
		tableNames := []string{"database_connection", "base_task", "task_chain",
			"chain_execution_config", "chain_execution_parameters",
			"log", "execution_log", "run_status"}
		for _, tableName := range tableNames {
			err := pgengine.ConfigDb.Get(&oid, fmt.Sprintf("SELECT COALESCE(to_regclass('timetable.%s'), 0) :: int", tableName))
			assert.NoError(t, err, fmt.Sprintf("Query for %s existance failed", tableName))
			assert.NotEqual(t, pgengine.InvalidOid, oid, fmt.Sprintf("timetable.%s function doesn't exist", tableName))
		}
	})

	t.Run("Check timetable functions", func(t *testing.T) {
		var oid int
		funcNames := []string{"_validate_json_schema_type(text, jsonb)",
			"validate_json_schema(jsonb, jsonb, jsonb)",
			"get_running_jobs(int)",
			"trig_chain_fixer()",
			"check_task(int)"}
		for _, funcName := range funcNames {
			err := pgengine.ConfigDb.Get(&oid, fmt.Sprintf("SELECT COALESCE(to_regprocedure('timetable.%s'), 0) :: int", funcName))
			assert.NoError(t, err, fmt.Sprintf("Query for %s existance failed", funcName))
			assert.NotEqual(t, pgengine.InvalidOid, oid, fmt.Sprintf("timetable.%s table doesn't exist", funcName))
		}
	})

	t.Run("Check log facility", func(t *testing.T) {
		var count int
		logLevels := []string{"DEBUG", "NOTICE", "LOG", "ERROR", "PANIC"}
		for _, pgengine.VerboseLogLevel = range []bool{true, false} {
			pgengine.ConfigDb.MustExec("TRUNCATE timetable.log")
			for _, logLevel := range logLevels {
				if logLevel == "PANIC" {
					assert.Panics(t, func() {
						pgengine.LogToDB(logLevel, logLevel)
					}, "LogToDB did not panic")
				} else {
					assert.NotPanics(t, func() {
						pgengine.LogToDB(logLevel, logLevel)
					}, "LogToDB panicked")
				}

				if !pgengine.VerboseLogLevel {
					switch logLevel {
					case "DEBUG", "NOTICE", "LOG":
						continue
					}
				}
				err := pgengine.ConfigDb.Get(&count, "SELECT count(1) FROM timetable.log WHERE log_level = $1 AND message = $2",
					logLevel, logLevel)
				assert.NoError(t, err, fmt.Sprintf("Query for %s log entry failed", logLevel))
				assert.Equal(t, 1, count, fmt.Sprintf("%s log entry doesn't exist", logLevel))
			}
		}
	})

	t.Run("Check connection closing", func(t *testing.T) {
		pgengine.FinalizeConfigDBConnection()
		assert.Nil(t, pgengine.ConfigDb, "Connection isn't closed properly")
		// reinit connection to execute teardown actions
		setupTestDBFunc()
	})
}

func TestSchedulerFunctions(t *testing.T) {
	teardownTestCase := setupTestCase(t)
	defer teardownTestCase(t)

	t.Run("Check FixSchedulerCrash function", func(t *testing.T) {
		assert.NotPanics(t, pgengine.FixSchedulerCrash, "Fix scheduler crash failed")
	})

	t.Run("Check CanProceedChainExecution funtion", func(t *testing.T) {
		assert.Equal(t, true, pgengine.CanProceedChainExecution(0, 0), "Should proceed with clean database")
	})

	t.Run("Check DeleteChainConfig funtion", func(t *testing.T) {
		tx := pgengine.StartTransaction()
		assert.Equal(t, false, pgengine.DeleteChainConfig(tx, 0), "Should not delete in clean database")
		pgengine.MustCommitTransaction(tx)
	})

	t.Run("Check GetChainElements funtion", func(t *testing.T) {
		var chains []pgengine.ChainElementExecution
		tx := pgengine.StartTransaction()
		assert.True(t, pgengine.GetChainElements(tx, &chains, 0), "Should no error in clean database")
		assert.Empty(t, chains, "Should be empty in clean database")
		pgengine.MustCommitTransaction(tx)
	})

	t.Run("Check GetChainParamValues funtion", func(t *testing.T) {
		var paramVals []string
		tx := pgengine.StartTransaction()
		assert.True(t, pgengine.GetChainParamValues(tx, &paramVals, &pgengine.ChainElementExecution{
			ChainID:     0,
			ChainConfig: 0}), "Should no error in clean database")
		assert.Empty(t, paramVals, "Should be empty in clean database")
		pgengine.MustCommitTransaction(tx)
	})

	t.Run("Check InsertChainRunStatus funtion", func(t *testing.T) {
		var id int
		tx := pgengine.StartTransaction()
		assert.NotPanics(t, func() { id = pgengine.InsertChainRunStatus(tx, 0, 0) }, "Should no error in clean database")
		assert.NotZero(t, id, "Run status id should be greater then 0")
		pgengine.MustCommitTransaction(tx)
	})

}

func TestBuiltInTasks(t *testing.T) {
	teardownTestCase := setupTestCase(t)
	defer teardownTestCase(t)
	t.Run("Check built-in tasks number", func(t *testing.T) {
		var num int
		err := pgengine.ConfigDb.Get(&num, "SELECT count(1) FROM timetable.base_task WHERE kind = 'BUILTIN'")
		assert.NoError(t, err, "Query for built-in tasks existance failed")
		assert.Equal(t, len(tasks.Tasks), num, fmt.Sprintf("Wrong number of built-in tasks: %d", num))
	})
}

func init() {
	for i := 0; i < len(pgengine.SQLSchemaFiles); i++ {
		pgengine.SQLSchemaFiles[i] = "../../sql/" + pgengine.SQLSchemaFiles[i]
	}
}
