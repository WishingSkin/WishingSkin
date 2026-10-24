package httpapi

import (
	"strconv"
	"testing"
)

func TestOfflinePlayerUUIDCompatibility(t *testing.T) {
	for _, test := range []struct{ name, want string }{
		{"FixturePlayer", "92d74dda76fe332cb669d0daddfb7952"},
		{"Steve", "5627dd98e6be3c21b8a8e92344183641"},
		{"Alex", "36532b5ec4423dbba24cc7e55d0f979a"},
		{"fixtureplayer", "0501375144273e2f8c42430889ccadf2"},
	} {
		if got := offlinePlayerUUID(test.name); got != test.want {
			t.Errorf("offlinePlayerUUID(%q) = %s, want %s", test.name, got, test.want)
		}
	}
}

func TestNewCharacterUsesOfflineUUIDAndRejectsUUIDCollisionBothDatabases(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			f := newManagementApp(t, dialect)
			created := webCharacter(t, f.call(1, "POST", "/api/characters", map[string]string{"name": "FixturePlayer"}))
			if created.UUID != "92d74dda76fe332cb669d0daddfb7952" {
				t.Fatalf("new character UUID: %s", created.UUID)
			}
			f.exec("INSERT INTO characters(id,user_id,uuid,name,name_key) VALUES(?,?,?,?,?)", int64(99), int64(2), offlinePlayerUUID("Collision"), "Existing", "existing")
			webStatus(t, f.call(1, "PATCH", "/api/characters/"+strconv.FormatInt(created.ID, 10), map[string]string{"name": "RenamedPlayer"}), 200)
			webStatus(t, f.call(3, "PATCH", "/api/admin/settings", map[string]int{"characterLimit": 2}), 200)
			if response := f.call(1, "POST", "/api/characters", map[string]string{"name": "Collision"}); response.Code != 409 {
				t.Fatalf("UUID collision status: %d %s", response.Code, response.Body)
			}
			var uuid string
			if err := f.db.DB.QueryRow(f.db.Bind("SELECT uuid FROM characters WHERE id=?"), created.ID).Scan(&uuid); err != nil || uuid != created.UUID {
				t.Fatalf("rename changed UUID: %s %v", uuid, err)
			}
		})
	}
}
