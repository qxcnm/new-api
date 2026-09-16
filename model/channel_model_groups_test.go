package model

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/glebarez/sqlite"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestChannelModelGroupsValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		input   *string
		want    map[string][]string
		invalid bool
	}{
		{name: "omitted"},
		{name: "cleared", input: common.GetPointer("")},
		{name: "empty object", input: common.GetPointer(`{}`)},
		{name: "restrictions", input: common.GetPointer(`{"gpt-4o":[" default ","vip"],"retired-model":["old-group"]}`), want: map[string][]string{"gpt-4o": {"default", "vip"}, "retired-model": {"old-group"}}},
		{name: "malformed", input: common.GetPointer(`{"gpt-4o":`), invalid: true},
		{name: "null object", input: common.GetPointer(`null`), invalid: true},
		{name: "array", input: common.GetPointer(`[]`), invalid: true},
		{name: "scalar", input: common.GetPointer(`"default"`), invalid: true},
		{name: "empty groups", input: common.GetPointer(`{"gpt-4o":[]}`), invalid: true},
		{name: "null groups", input: common.GetPointer(`{"gpt-4o":null}`), invalid: true},
		{name: "group scalar", input: common.GetPointer(`{"gpt-4o":"vip"}`), invalid: true},
		{name: "empty group", input: common.GetPointer(`{"gpt-4o":[" "]}`), invalid: true},
		{name: "duplicate normalized group", input: common.GetPointer(`{"gpt-4o":["vip"," vip "]}`), invalid: true},
		{name: "empty model", input: common.GetPointer(`{" ":["vip"]}`), invalid: true},
		{name: "padded model", input: common.GetPointer(`{" gpt-4o":["vip"]}`), invalid: true},
		{name: "oversized group", input: common.GetPointer(`{"gpt-4o":["` + strings.Repeat("g", 65) + `"]}`), invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			channel := Channel{Models: "gpt-4o", Group: "default,vip", ModelGroups: tc.input}
			got, err := channel.GetModelGroups()
			if tc.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if len(tc.want) == 0 {
				assert.Empty(t, got)
			} else {
				assert.Equal(t, tc.want, got)
			}
		})
	}
}

// The persisted Channel schema from release v1.0.0-rc.37, before model_groups.
// An explicit released fixture exercises a real upgrade with existing data.
type channelBeforeModelGroups struct {
	Id                 int
	Type               int    `gorm:"default:0"`
	Key                string `gorm:"not null"`
	OpenAIOrganization *string
	TestModel          *string
	Status             int    `gorm:"default:1"`
	Name               string `gorm:"index"`
	Weight             *uint  `gorm:"default:0"`
	CreatedTime        int64  `gorm:"bigint"`
	TestTime           int64  `gorm:"bigint"`
	ResponseTime       int
	BaseURL            *string `gorm:"column:base_url;default:''"`
	Other              string
	Balance            float64
	BalanceUpdatedTime int64 `gorm:"bigint"`
	Models             string
	Group              string  `gorm:"type:varchar(64);default:'default'"`
	UsedQuota          int64   `gorm:"bigint;default:0"`
	ModelMapping       *string `gorm:"type:text"`
	StatusCodeMapping  *string `gorm:"type:varchar(1024);default:''"`
	Priority           *int64  `gorm:"bigint;default:0"`
	AutoBan            *int    `gorm:"default:1"`
	OtherInfo          string
	Tag                *string     `gorm:"index"`
	Setting            *string     `gorm:"type:text"`
	ParamOverride      *string     `gorm:"type:text"`
	HeaderOverride     *string     `gorm:"type:text"`
	Remark             *string     `gorm:"type:varchar(255)"`
	ChannelInfo        ChannelInfo `gorm:"type:json"`
	OtherSettings      string      `gorm:"column:settings"`
}

func (channelBeforeModelGroups) TableName() string { return "channels" }

// External DSNs must belong to test servers. Each fixture creates and drops its
// own database (MySQL) or schema (PostgreSQL), preserving all existing data.
func channelModelGroupsDatabase(t *testing.T, dialect common.DatabaseType, dsn string) *gorm.DB {
	t.Helper()
	config := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	var db *gorm.DB
	var err error
	if dialect == common.DatabaseTypeSQLite {
		db, err = gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channels.db")), config)
		require.NoError(t, err)
	} else {
		name := fmt.Sprintf("channel_groups_%d", time.Now().UnixNano())
		var admin *gorm.DB
		if dialect == common.DatabaseTypeMySQL {
			admin, err = gorm.Open(mysql.Open(dsn), config)
		} else {
			admin, err = gorm.Open(postgres.Open(dsn), config)
		}
		require.NoError(t, err)
		adminSQL, adminErr := admin.DB()
		require.NoError(t, adminErr)
		t.Cleanup(func() { require.NoError(t, adminSQL.Close()) })
		if dialect == common.DatabaseTypeMySQL {
			require.NoError(t, admin.Exec("CREATE DATABASE `"+name+"`").Error)
			t.Cleanup(func() { require.NoError(t, admin.Exec("DROP DATABASE `"+name+"`").Error) })
			parsed, parseErr := mysqldriver.ParseDSN(dsn)
			require.NoError(t, parseErr)
			parsed.DBName = name
			db, err = gorm.Open(mysql.Open(parsed.FormatDSN()), config)
		} else {
			require.NoError(t, admin.Exec(`CREATE SCHEMA "`+name+`"`).Error)
			t.Cleanup(func() { require.NoError(t, admin.Exec(`DROP SCHEMA "`+name+`" CASCADE`).Error) })
			if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
				parsed, parseErr := url.Parse(dsn)
				require.NoError(t, parseErr)
				query := parsed.Query()
				query.Set("search_path", name)
				parsed.RawQuery = query.Encode()
				dsn = parsed.String()
			} else {
				dsn += " search_path=" + name
			}
			db, err = gorm.Open(postgres.Open(dsn), config)
		}
		require.NoError(t, err)
	}
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	previousDB, previousLogDB := DB, LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousMemoryCache := common.MemoryCacheEnabled
	DB, LOG_DB = db, db
	common.SetDatabaseTypes(dialect, dialect)
	initCol()
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		common.MemoryCacheEnabled = previousMemoryCache
		initCol()
		InitChannelCache()
	})
	var version string
	query := "SELECT version()"
	if dialect == common.DatabaseTypeSQLite {
		query = "SELECT sqlite_version()"
	}
	require.NoError(t, db.Raw(query).Scan(&version).Error)
	t.Logf("database version: %s", version)
	return db
}

func assertChannelModelRoutes(t *testing.T, channelID int, expected map[string][]string) {
	t.Helper()
	for _, memoryCache := range []bool{false, true} {
		common.MemoryCacheEnabled = memoryCache
		InitChannelCache()
		for _, group := range []string{"default", "vip"} {
			assert.ElementsMatch(t, expected[group], GetGroupEnabledModels(group), "group=%s cache=%t", group, memoryCache)
			for _, modelName := range []string{"gpt-4o", "gpt-4o-mini", "shared-model"} {
				want := false
				for _, enabled := range expected[group] {
					want = want || enabled == modelName
				}
				selected, err := GetRandomSatisfiedChannel(group, modelName, 0, nil)
				require.NoError(t, err)
				if want {
					require.NotNil(t, selected, "group=%s model=%s cache=%t", group, modelName, memoryCache)
					assert.Equal(t, channelID, selected.Id)
				} else {
					assert.Nil(t, selected, "group=%s model=%s cache=%t", group, modelName, memoryCache)
				}
				assert.Equal(t, want, IsChannelEnabledForGroupModel(group, modelName, channelID), "group=%s model=%s cache=%t", group, modelName, memoryCache)
			}
		}
		pricingGroups := make(map[string][]string)
		for _, pricing := range GetPricing() {
			pricingGroups[pricing.ModelName] = pricing.EnableGroup
		}
		for _, modelName := range []string{"gpt-4o", "gpt-4o-mini", "shared-model"} {
			groups := []string{}
			for group, models := range expected {
				for _, enabled := range models {
					if enabled == modelName {
						groups = append(groups, group)
					}
				}
			}
			assert.ElementsMatch(t, groups, pricingGroups[modelName], "pricing model=%s cache=%t", modelName, memoryCache)
		}
	}
}

func TestChannelModelGroupsDatabases(t *testing.T) {
	for _, dialect := range []struct {
		database common.DatabaseType
		env      string
	}{
		{common.DatabaseTypeSQLite, ""},
		{common.DatabaseTypeMySQL, "CHANNEL_MODEL_GROUPS_MYSQL_DSN"},
		{common.DatabaseTypePostgreSQL, "CHANNEL_MODEL_GROUPS_POSTGRES_DSN"},
	} {
		t.Run(string(dialect.database), func(t *testing.T) {
			dsn := os.Getenv(dialect.env)
			if dialect.env != "" && dsn == "" {
				t.Skip(dialect.env + " is not configured")
			}
			for _, upgrade := range []bool{false, true} {
				t.Run(fmt.Sprintf("upgrade=%t", upgrade), func(t *testing.T) {
					db := channelModelGroupsDatabase(t, dialect.database, dsn)
					var legacyID int
					if upgrade {
						require.NoError(t, db.AutoMigrate(&channelBeforeModelGroups{}, &Ability{}))
						legacy := channelBeforeModelGroups{Key: "migration-fixture-key", Name: "legacy", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: "gpt-4o,gpt-4o-mini,shared-model", Group: "default,vip", UsedQuota: 12345, Tag: common.GetPointer("retained")}
						require.NoError(t, db.Create(&legacy).Error)
						legacyID = legacy.Id
						channel := Channel{Id: legacyID, Models: legacy.Models, Group: legacy.Group, Status: legacy.Status}
						require.NoError(t, channel.AddAbilities(nil))
					}
					require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}, &Model{}, &Vendor{}))
					recorder := &migrationSQLRecorder{}
					require.NoError(t, db.Session(&gorm.Session{Logger: recorder}).AutoMigrate(&Channel{}, &Ability{}))
					assert.Empty(t, recorder.schemaMutations(), "second channel migration must not alter the schema")
					if upgrade {
						var legacy Channel
						require.NoError(t, db.First(&legacy, legacyID).Error)
						assert.Nil(t, legacy.ModelGroups)
						assert.Equal(t, "migration-fixture-key", legacy.Key)
						assert.Equal(t, int64(12345), legacy.UsedQuota)
						require.NotNil(t, legacy.Tag)
						assert.Equal(t, "retained", *legacy.Tag)
						assertChannelModelRoutes(t, legacyID, map[string][]string{"default": {"gpt-4o", "gpt-4o-mini", "shared-model"}, "vip": {"gpt-4o", "gpt-4o-mini", "shared-model"}})
						require.Error(t, db.Create(&Ability{Group: "default", Model: "gpt-4o", ChannelId: legacyID, Enabled: true}).Error, "existing composite uniqueness must survive migration")
						require.True(t, db.Migrator().HasIndex(&Channel{}, "Name"))
						require.True(t, db.Migrator().HasIndex(&Channel{}, "Tag"))
						require.NoError(t, db.Where("channel_id = ?", legacyID).Delete(&Ability{}).Error)
						require.NoError(t, db.Delete(&legacy).Error)
					}
					channel := Channel{Type: constant.ChannelTypeOpenAI, Name: "provider", Key: "fixture-key", Status: common.ChannelStatusEnabled, Group: "default,vip", Models: "gpt-4o,gpt-4o-mini,shared-model", ModelGroups: common.GetPointer(`{"gpt-4o":["vip"],"gpt-4o-mini":["default"]}`)}
					require.NoError(t, channel.Insert())
					restricted := map[string][]string{"default": {"gpt-4o-mini", "shared-model"}, "vip": {"gpt-4o", "shared-model"}}
					assertChannelModelRoutes(t, channel.Id, restricted)
					partial := Channel{Id: channel.Id, Name: "provider renamed"}
					require.NoError(t, partial.Update())
					assertChannelModelRoutes(t, channel.Id, restricted)
					successful, failed, err := FixAbility()
					require.NoError(t, err)
					assert.Equal(t, 1, successful)
					assert.Zero(t, failed)
					assertChannelModelRoutes(t, channel.Id, restricted)

					tagged := Channel{Id: channel.Id, Tag: common.GetPointer("supplier-batch")}
					require.NoError(t, tagged.Update())
					require.NoError(t, EditChannelByTag("supplier-batch", common.GetPointer("renamed-batch"), nil, nil, common.GetPointer("default"), nil, nil, nil, nil))
					assertChannelModelRoutes(t, channel.Id, map[string][]string{"default": {"gpt-4o-mini", "shared-model"}})
					var renamed Channel
					require.NoError(t, db.First(&renamed, channel.Id).Error)
					assert.Equal(t, "renamed-batch", renamed.GetTag())
					assert.Equal(t, channel.ModelGroups, renamed.ModelGroups, "tag/group changes must retain model restrictions")
					var renamedAbilities []Ability
					require.NoError(t, db.Where("channel_id = ?", channel.Id).Find(&renamedAbilities).Error)
					for _, ability := range renamedAbilities {
						assert.Equal(t, common.GetPointer("renamed-batch"), ability.Tag)
					}
					require.NoError(t, EditChannelByTag("renamed-batch", nil, nil, nil, common.GetPointer("default,vip"), nil, nil, nil, nil))
					assertChannelModelRoutes(t, channel.Id, restricted)

					second := Channel{Type: constant.ChannelTypeOpenAI, Name: "second provider", Key: "second-fixture", Status: common.ChannelStatusEnabled, Models: "other-model", Group: "default,vip", Tag: common.GetPointer("renamed-batch")}
					require.NoError(t, second.Insert())
					require.NoError(t, db.Model(&second).Update("model_groups", `{"other-model":[]}`).Error)
					var beforeBatch []Channel
					var beforeAbilities []Ability
					require.NoError(t, db.Order("id").Find(&beforeBatch).Error)
					require.NoError(t, db.Order("channel_id, model, "+commonGroupCol).Find(&beforeAbilities).Error)
					require.Error(t, EditChannelByTag("renamed-batch", common.GetPointer("must-not-persist"), nil, nil, common.GetPointer("vip"), common.GetPointer(int64(42)), nil, nil, nil))
					var afterBatch []Channel
					var afterAbilities []Ability
					require.NoError(t, db.Order("id").Find(&afterBatch).Error)
					require.NoError(t, db.Order("channel_id, model, "+commonGroupCol).Find(&afterAbilities).Error)
					assert.Equal(t, beforeBatch, afterBatch, "invalid batch member must roll back every channel edit")
					assert.Equal(t, beforeAbilities, afterAbilities, "invalid batch member must roll back all ability changes")
					require.NoError(t, db.Where("channel_id = ?", second.Id).Delete(&Ability{}).Error)
					require.NoError(t, db.Delete(&second).Error)
					assertChannelModelRoutes(t, channel.Id, restricted)

					invalid := Channel{Id: channel.Id, Name: "must roll back", ModelGroups: common.GetPointer(`{"gpt-4o":[]}`)}
					require.Error(t, invalid.Update())
					var persisted Channel
					require.NoError(t, db.First(&persisted, channel.Id).Error)
					assert.Equal(t, "provider renamed", persisted.Name)
					assertChannelModelRoutes(t, channel.Id, restricted)
					persisted.ModelGroups = common.GetPointer(`{"gpt-4o":`)
					require.Error(t, persisted.UpdateAbilities(nil))
					assertChannelModelRoutes(t, channel.Id, restricted)
					invalid = Channel{Key: "invalid-fixture", Models: "gpt-4o", Group: "default", ModelGroups: common.GetPointer(`{"gpt-4o":null}`)}
					require.Error(t, invalid.Insert())
					invalid.Id = 0
					require.Error(t, BatchInsertChannels([]Channel{
						{Key: "batch-fixture", Models: "gpt-4o", Group: "default"},
						invalid,
					}))
					var count int64
					require.NoError(t, db.Model(&Channel{}).Count(&count).Error)
					assert.Equal(t, int64(1), count)
					removeModel := Channel{Id: channel.Id, Models: "gpt-4o-mini,shared-model"}
					require.NoError(t, removeModel.Update())
					assertChannelModelRoutes(t, channel.Id, map[string][]string{"default": {"gpt-4o-mini", "shared-model"}, "vip": {"shared-model"}})
					restoreModel := Channel{Id: channel.Id, Models: "gpt-4o,gpt-4o-mini,shared-model"}
					require.NoError(t, restoreModel.Update())
					assertChannelModelRoutes(t, channel.Id, restricted)
					removeGroup := Channel{Id: channel.Id, Group: "default"}
					require.NoError(t, removeGroup.Update())
					assertChannelModelRoutes(t, channel.Id, map[string][]string{"default": {"gpt-4o-mini", "shared-model"}})
					clear := Channel{Id: channel.Id, Group: "default,vip", ModelGroups: common.GetPointer("")}
					require.NoError(t, clear.Update())
					assertChannelModelRoutes(t, channel.Id, map[string][]string{"default": {"gpt-4o", "gpt-4o-mini", "shared-model"}, "vip": {"gpt-4o", "gpt-4o-mini", "shared-model"}})
					require.NoError(t, UpdateAbilityStatus(channel.Id, false))
					assertChannelModelRoutes(t, channel.Id, nil)
				})
			}
		})
	}
}
