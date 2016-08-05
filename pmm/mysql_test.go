package pmm

import (
	"regexp"
	"testing"

	//"github.com/percona/go-mysql/dsn"
	"github.com/smartystreets/goconvey/convey"
	"gopkg.in/DATA-DOG/go-sqlmock.v1"
)

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

//func TestMakeGrant(t *testing.T) {
//	type sample struct {
//		dsn     dsn.DSN
//		grants  []string
//		service string
//		source  string
//		conn    uint
//	}
//	samples := []sample{
//		{dsn: dsn.DSN{Username: "root", Password: "abc123", Hostname: "localhost", Socket: ""},
//			service: "mysql",
//			source:  "",
//			conn:    5,
//			grants: []string{"SET SESSION old_passwords=0",
//				"GRANT PROCESS, REPLICATION CLIENT ON *.* TO 'root'@'localhost' IDENTIFIED BY 'abc123' WITH MAX_USER_CONNECTIONS 5",
//				"GRANT SELECT ON `performance_schema`.* TO 'root'@'localhost'"},
//		},
//		{dsn: dsn.DSN{Username: "admin", Password: "23;,_-asd", Hostname: "127.0.0.1", Socket: ""},
//			service: "mysql",
//			source:  "",
//			conn:    10,
//			grants: []string{"SET SESSION old_passwords=0",
//				"GRANT PROCESS, REPLICATION CLIENT ON *.* TO 'admin'@'127.0.0.1' IDENTIFIED BY '23;,_-asd' WITH MAX_USER_CONNECTIONS 10",
//				"GRANT SELECT ON `performance_schema`.* TO 'admin'@'127.0.0.1'"},
//		},
//		{dsn: dsn.DSN{Username: "root", Password: "abc123", Hostname: "1.2.3.4", Socket: "/var/lib/mysql/mysql.sock"},
//			service: "mysql",
//			source:  "",
//			conn:    5,
//			grants: []string{"SET SESSION old_passwords=0",
//				"GRANT PROCESS, REPLICATION CLIENT ON *.* TO 'root'@'localhost' IDENTIFIED BY 'abc123' WITH MAX_USER_CONNECTIONS 5",
//				"GRANT SELECT ON `performance_schema`.* TO 'root'@'localhost'"},
//		},
//		{dsn: dsn.DSN{Username: "root", Password: "abc123", Hostname: "1.2.3.4", Socket: ""},
//			service: "mysql",
//			source:  "",
//			conn:    5,
//			grants: []string{"SET SESSION old_passwords=0",
//				"GRANT PROCESS, REPLICATION CLIENT ON *.* TO 'root'@'%' IDENTIFIED BY 'abc123' WITH MAX_USER_CONNECTIONS 5",
//				"GRANT SELECT ON `performance_schema`.* TO 'root'@'%'"},
//		},
//		{dsn: dsn.DSN{Username: "root", Password: "abc123", Hostname: "1.2.3.4", Socket: ""},
//			service: "queries",
//			source:  "auto",
//			conn:    5,
//			grants: []string{"SET SESSION old_passwords=0",
//				"GRANT SELECT, PROCESS ON *.* TO 'root'@'%' IDENTIFIED BY 'abc123' WITH MAX_USER_CONNECTIONS 5",
//				"GRANT SELECT, UPDATE, DELETE, DROP ON `performance_schema`.* TO 'root'@'%'"},
//		},
//		{dsn: dsn.DSN{Username: "pmm-queries", Password: "12345", Hostname: "1.2.3.4", Socket: ""},
//			service: "queries",
//			source:  "slowlog",
//			conn:    5,
//			grants: []string{"SET SESSION old_passwords=0",
//				"GRANT SELECT, PROCESS, SUPER ON *.* TO 'pmm-queries'@'%' IDENTIFIED BY '12345' WITH MAX_USER_CONNECTIONS 5",
//				"GRANT SELECT, UPDATE, DELETE, DROP ON `performance_schema`.* TO 'pmm-queries'@'%'"},
//		},
//		{dsn: dsn.DSN{Username: "pmm-queries", Password: "12345", Hostname: "localhost", Socket: ""},
//			service: "queries",
//			source:  "perfschema",
//			conn:    5,
//			grants: []string{"SET SESSION old_passwords=0",
//				"GRANT SELECT, PROCESS ON *.* TO 'pmm-queries'@'localhost' IDENTIFIED BY '12345' WITH MAX_USER_CONNECTIONS 5",
//				"GRANT SELECT, UPDATE, DELETE, DROP ON `performance_schema`.* TO 'pmm-queries'@'localhost'"},
//		},
//	}
//	convey.Convey("Making grants", t, func() {
//		for _, s := range samples {
//			convey.So(makeGrant(s.dsn, s.source, s.conn), convey.ShouldResemble, s.grants)
//		}
//	})
//}

func TestGetMysqlInfo(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("error opening a stub database connection: %s", err)
	}
	defer db.Close()

	columns := []string{"@@hostname", "@@port", "@@version_comment", "@@version"}
	rows := sqlmock.NewRows(columns).AddRow("db01", "3306", "MySQL", "1.2.3")
	mock.ExpectQuery("SELECT @@hostname, @@port, @@version_comment, @@version").WillReturnRows(rows)

	res, _ := getMysqlInfo(db)
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
