package pmm

import (
	"regexp"
	"testing"

	"github.com/percona/go-mysql/dsn"
	"github.com/smartystreets/goconvey/convey"
	"gopkg.in/DATA-DOG/go-sqlmock.v1"
)

func TestMySQLCheck1(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("error opening a stub database connection: %s", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows([]string{"col1"}).AddRow("0")
	mock.ExpectQuery("SELECT @@read_only").WillReturnRows(rows)

	rows = sqlmock.NewRows([]string{"col1"})
	mock.ExpectQuery("SHOW SLAVE STATUS").WillReturnRows(rows)

	mock.ExpectQuery("SHOW GRANTS FOR 'pmm'@'localhost'").WillReturnError(err)

	convey.Convey("MySQL checks OK", t, func() {
		convey.So(mysqlCheck(db, []string{"localhost"}), convey.ShouldBeNil)
	})

	// Ensure all SQL queries were executed
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expections: %s", err)
	}
}

func TestMySQLCheck2(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("error opening a stub database connection: %s", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows([]string{"col1"}).AddRow("1")
	mock.ExpectQuery("SELECT @@read_only").WillReturnRows(rows)

	rows = sqlmock.NewRows([]string{"col1"})
	mock.ExpectQuery("SHOW SLAVE STATUS").WillReturnRows(rows)

	mock.ExpectQuery("SHOW GRANTS FOR 'pmm'@'localhost'").WillReturnError(err)

	convey.Convey("MySQL checks FAIL", t, func() {
		convey.So(mysqlCheck(db, []string{"localhost"}), convey.ShouldNotBeNil)
	})

	// Ensure all SQL queries were executed
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expections: %s", err)
	}
}

func TestMySQLCheck3(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("error opening a stub database connection: %s", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows([]string{"col1"}).AddRow("0")
	mock.ExpectQuery("SELECT @@read_only").WillReturnRows(rows)

	rows = sqlmock.NewRows([]string{"col1"}).AddRow("1")
	mock.ExpectQuery("SHOW SLAVE STATUS").WillReturnRows(rows)

	mock.ExpectQuery("SHOW GRANTS FOR 'pmm'@'localhost'").WillReturnError(err)

	convey.Convey("MySQL checks FAIL", t, func() {
		convey.So(mysqlCheck(db, []string{"localhost"}), convey.ShouldNotBeNil)
	})

	// Ensure all SQL queries were executed
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expections: %s", err)
	}
}

func TestMySQLCheck4(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("error opening a stub database connection: %s", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows([]string{"col1"}).AddRow("0")
	mock.ExpectQuery("SELECT @@read_only").WillReturnRows(rows)

	rows = sqlmock.NewRows([]string{"col1"})
	mock.ExpectQuery("SHOW SLAVE STATUS").WillReturnRows(rows)

	rows = sqlmock.NewRows([]string{"col1"})
	mock.ExpectQuery("SHOW GRANTS FOR 'pmm'@'localhost'").WillReturnRows(rows)

	convey.Convey("MySQL checks FAIL", t, func() {
		convey.So(mysqlCheck(db, []string{"localhost"}), convey.ShouldNotBeNil)
	})

	// Ensure all SQL queries were executed
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expections: %s", err)
	}
}

func TestMakeGrants(t *testing.T) {
	type sample struct {
		dsn    dsn.DSN
		hosts  []string
		conn   uint
		grants []string
	}
	samples := []sample{
		{dsn: dsn.DSN{Username: "root", Password: "abc123"},
			hosts: []string{"localhost", "127.0.0.1"},
			conn:  5,
			grants: []string{
				"GRANT SELECT, PROCESS, REPLICATION CLIENT, SUPER ON *.* TO 'root'@'localhost' IDENTIFIED BY 'abc123' WITH MAX_USER_CONNECTIONS 5",
				"GRANT UPDATE, DELETE, DROP ON `performance_schema`.* TO 'root'@'localhost'",
				"GRANT SELECT, PROCESS, REPLICATION CLIENT, SUPER ON *.* TO 'root'@'127.0.0.1' IDENTIFIED BY 'abc123' WITH MAX_USER_CONNECTIONS 5",
				"GRANT UPDATE, DELETE, DROP ON `performance_schema`.* TO 'root'@'127.0.0.1'",
			},
		},
		{dsn: dsn.DSN{Username: "admin", Password: "23;,_-asd"},
			hosts: []string{"%"},
			conn:  20,
			grants: []string{
				"GRANT SELECT, PROCESS, REPLICATION CLIENT, SUPER ON *.* TO 'admin'@'%' IDENTIFIED BY '23;,_-asd' WITH MAX_USER_CONNECTIONS 20",
				"GRANT UPDATE, DELETE, DROP ON `performance_schema`.* TO 'admin'@'%'",
			},
		},
	}
	convey.Convey("Making grants", t, func() {
		for _, s := range samples {
			convey.So(makeGrants(s.dsn, s.hosts, s.conn), convey.ShouldResemble, s.grants)
		}
	})
}

func TestGetMysqlInfo(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("error opening a stub database connection: %s", err)
	}
	defer db.Close()

	columns := []string{"@@hostname", "@@port", "@@version_comment", "@@version"}
	rows := sqlmock.NewRows(columns).AddRow("db01", "3306", "MySQL", "1.2.3")
	mock.ExpectQuery("SELECT @@hostname, @@port, @@version_comment, @@version").WillReturnRows(rows)

	res := getMysqlInfo(db)
	expect := map[string]string{
		"hostname": "db01",
		"port":     "3306",
		"distro":   "MySQL",
		"version":  "1.2.3",
	}
	convey.Convey("Get MySQL info", t, func() {
		convey.So(res, convey.ShouldResemble, expect)
	})

	// Ensure all SQL queries were executed
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expections: %s", err)
	}
}

func TestGeneratePassword(t *testing.T) {
	r, _ := regexp.Compile("^([[:alnum:]]|[_,;-]){20}$")
	r1, _ := regexp.Compile("[[:lower:]]")
	r2, _ := regexp.Compile("[[:upper:]]")
	r3, _ := regexp.Compile("[[:digit:]]")
	r4, _ := regexp.Compile("[_,;-]")

	convey.Convey("Password generation", t, func() {
		convey.So(generatePassword(5), convey.ShouldHaveLength, 5)
		convey.So(generatePassword(20), convey.ShouldHaveLength, 20)
		convey.So(generatePassword(20), convey.ShouldNotEqual, generatePassword(20))
		for i := 0; i < 10; i++ {
			p := generatePassword(20)
			c := r.Match([]byte(p)) && r1.Match([]byte(p)) && r2.Match([]byte(p)) && r3.Match([]byte(p)) && r4.Match([]byte(p))
			convey.So(c, convey.ShouldBeTrue)
		}
	})
}
