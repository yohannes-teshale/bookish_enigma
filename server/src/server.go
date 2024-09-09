package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"github.com/rs/cors"
	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
	"github.com/spf13/viper"
)

type Config struct {
	ConnectionString string   `mapstructure:"db_connection_string"`
	TargetTables     []string `mapstructure:"target_tables"`
	TargetUsers      []string `mapstructure:"target_users"`
	Port             int      `mapstructure:"port"`
}

type AuditLog struct {
	ID           int             `json:"id"`
	TargetTableID int             `json:"targetTableId"`
	Username     string          `json:"username"`
	OldValue     json.RawMessage `json:"oldValue"`
	NewValue     json.RawMessage `json:"newValue"`
	Operation    string          `json:"operation"`
	Timestamp    string          `json:"timestamp"`
}

var (
	db     *sql.DB
	config Config
)

func main() {
	loadConfig()
	initDB()
	defer db.Close()

	setupDatabase()
	startServer()
}

func loadConfig() {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig()
	if err != nil {
		log.Fatalf("Error reading config file: %s", err)
	}

	err = viper.Unmarshal(&config)
	if err != nil {
		log.Fatalf("Unable to decode config into struct: %s", err)
	}
}

func initDB() {
	var err error
	db, err = sql.Open("postgres", config.ConnectionString)
	if err != nil {
		log.Fatalf("Error opening database connection: %s", err)
	}

	err = db.Ping()
	if err != nil {
		log.Fatalf("Error connecting to the database: %s", err)
	}
}

func setupDatabase() {
	createAuditTable()
	createTriggerFunction()
	createTriggers(config.TargetTables)
}

func createAuditTable() {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS audit_logs (
			id SERIAL PRIMARY KEY,
			target_table_id INTEGER,
			username TEXT,
			old_value JSONB,
			new_value JSONB,
			operation TEXT,
			timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		log.Fatalf("Error creating audit table: %s", err)
	}
}

func createTriggerFunction() {
	_, err := db.Exec(`
		CREATE OR REPLACE FUNCTION audit_trigger_func()
		RETURNS TRIGGER AS $$
		BEGIN
			IF (TG_OP = 'UPDATE') THEN
				INSERT INTO audit_logs (target_table_id, username, old_value, new_value, operation)
				VALUES (OLD.id, current_user, row_to_json(OLD), row_to_json(NEW), TG_OP);
			ELSIF (TG_OP = 'DELETE') THEN
				INSERT INTO audit_logs (target_table_id, username, old_value, operation)
				VALUES (OLD.id, current_user, row_to_json(OLD), TG_OP);
			ELSIF (TG_OP = 'INSERT') THEN
				INSERT INTO audit_logs (target_table_id, username, new_value, operation)
				VALUES (NEW.id, current_user, row_to_json(NEW), TG_OP);
			END IF;
			RETURN NULL;
		END;
		$$ LANGUAGE plpgsql;
	`)
	if err != nil {
		log.Fatalf("Error creating trigger function: %s", err)
	}
}

func createTriggers(targetTables []string) {
	for _, table := range targetTables {
		_, err := db.Exec(`
        			DROP TRIGGER IF EXISTS audit_trigger ON users;`)
    		if err != nil {
        		log.Fatalf("Error dropping existing trigger: %s", err)
    		}
		_, err= db.Exec(fmt.Sprintf(`
			CREATE TRIGGER audit_trigger
			AFTER INSERT OR UPDATE OR DELETE ON %s
			FOR EACH ROW EXECUTE FUNCTION audit_trigger_func();
		`, table))
		if err != nil {
			log.Fatalf("Error creating trigger for table %s: %s", table, err)
		}
	}
}

func startServer() {
	r := mux.NewRouter()
	r.HandleFunc("/api/logs", getAuditLogs).Methods("GET")
	r.HandleFunc("/api/logs/{id}", getAuditLog).Methods("GET")
	r.HandleFunc("/api/revert/{id}", revertChange).Methods("POST")
c := cors.New(cors.Options{
        AllowedOrigins: []string{"http://localhost:5173"},
        AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
        AllowedHeaders: []string{"*"},
    })

    handler := c.Handler(r)

    log.Printf("Starting server on port %d", config.Port)
    log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", config.Port), handler))
	port := fmt.Sprintf(":%d", config.Port)
	log.Printf("Starting server on port %s", port)
	log.Fatal(http.ListenAndServe(port, r))
}

func getAuditLogs(w http.ResponseWriter, r *http.Request) {
	limit := r.URL.Query().Get("limit")
	offset := r.URL.Query().Get("offset")

	query := `
		SELECT id, target_table_id, username, old_value, new_value, operation, timestamp
		FROM audit_logs
		ORDER BY timestamp DESC
	`

	if limit != "" {
		query += fmt.Sprintf(" LIMIT %s", limit)
	}
	if offset != "" {
		query += fmt.Sprintf(" OFFSET %s", offset)
	}

	rows, err := db.Query(query)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error querying audit logs: %s", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var logs []AuditLog
	for rows.Next() {
		var log AuditLog
		err := rows.Scan(&log.ID, &log.TargetTableID, &log.Username, &log.OldValue, &log.NewValue, &log.Operation, &log.Timestamp)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error scanning audit log: %s", err), http.StatusInternalServerError)
			return
		}
		logs = append(logs, log)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(logs)
}

func getAuditLog(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid audit log ID", http.StatusBadRequest)
		return
	}

	var log AuditLog
	err = db.QueryRow(`
		SELECT id, target_table_id, username, old_value, new_value, operation, timestamp
		FROM audit_logs
		WHERE id = $1
	`, id).Scan(&log.ID, &log.TargetTableID, &log.Username, &log.OldValue, &log.NewValue, &log.Operation, &log.Timestamp)

	if err == sql.ErrNoRows {
		http.Error(w, "Audit log not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, fmt.Sprintf("Error querying audit log: %s", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(log)
}

func revertChange(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid audit log ID", http.StatusBadRequest)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, fmt.Sprintf("Error starting transaction: %s", err), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var targetTableID int
	var operation string
	var oldValue, newValue json.RawMessage

	err = tx.QueryRow(`
		SELECT target_table_id, operation, old_value, new_value
		FROM audit_logs
		WHERE id = $1
	`, id).Scan(&targetTableID, &operation, &oldValue, &newValue)

	if err == sql.ErrNoRows {
		http.Error(w, "Audit log not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, fmt.Sprintf("Error querying audit log: %s", err), http.StatusInternalServerError)
		return
	}

	var tableName string
	err = tx.QueryRow("SELECT table_name FROM target_tables WHERE id = $1", targetTableID).Scan(&tableName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error getting table name: %s", err), http.StatusInternalServerError)
		return
	}

	switch operation {
	case "UPDATE", "DELETE":
		_, err = tx.Exec(fmt.Sprintf(`
			INSERT INTO %s SELECT * FROM json_populate_record(null::%s, $1)
			ON CONFLICT (id) DO UPDATE
			SET (SELECT string_agg(format('%%I = EXCLUDED.%%I', key, key), ', ')
				FROM json_object_keys($1::json) AS key)
		`, tableName, tableName), oldValue)
	case "INSERT":
		var id int
		err = json.Unmarshal(oldValue, &id)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error parsing old value: %s", err), http.StatusInternalServerError)
			return
		}
		_, err = tx.Exec(fmt.Sprintf("DELETE FROM %s WHERE id = $1", tableName), id)
	default:
		http.Error(w, "Invalid operation", http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, fmt.Sprintf("Error reverting change: %s", err), http.StatusInternalServerError)
		return
	}

	err = tx.Commit()
	if err != nil {
		http.Error(w, fmt.Sprintf("Error committing transaction: %s", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Change reverted successfully")
}

func init() {
	log.SetOutput(os.Stdout)
}
