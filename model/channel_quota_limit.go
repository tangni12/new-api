package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ChannelQuotaLimit stores channel spending limit settings outside the channels table.
type ChannelQuotaLimit struct {
	Id          int   `json:"id" gorm:"primaryKey"`
	ChannelId   int   `json:"channel_id" gorm:"uniqueIndex;not null"`
	Enabled     bool  `json:"enabled" gorm:"default:false;not null"`
	QuotaLimit  int64 `json:"quota_limit" gorm:"bigint;default:0;not null"`
	CreatedTime int64 `json:"created_time" gorm:"bigint"`
	UpdatedTime int64 `json:"updated_time" gorm:"bigint"`
}

func (ChannelQuotaLimit) TableName() string {
	return "channel_quota_limits"
}

type channelQuotaLimitCache struct {
	Exists     bool
	Enabled    bool
	QuotaLimit int64
}

func getChannelQuotaLimitCacheKey(channelId int) string {
	return fmt.Sprintf("channel_quota_limit:%d", channelId)
}

func getChannelQuotaLimitFromCache(channelId int) (*ChannelQuotaLimit, error) {
	if !common.RedisEnabled {
		return nil, fmt.Errorf("redis is not enabled")
	}
	cache := channelQuotaLimitCache{}
	if err := common.RedisHGetObj(getChannelQuotaLimitCacheKey(channelId), &cache); err != nil {
		return nil, err
	}
	if !cache.Exists {
		return nil, gorm.ErrRecordNotFound
	}
	return &ChannelQuotaLimit{
		ChannelId:  channelId,
		Enabled:    cache.Enabled,
		QuotaLimit: cache.QuotaLimit,
	}, nil
}

func setChannelQuotaLimitCache(channelId int, exists bool, enabled bool, quotaLimit int64) error {
	if !common.RedisEnabled || channelId == 0 {
		return nil
	}
	if !exists || !enabled || quotaLimit <= 0 {
		enabled = false
		quotaLimit = 0
		exists = false
	}
	cache := channelQuotaLimitCache{
		Exists:     exists,
		Enabled:    enabled,
		QuotaLimit: quotaLimit,
	}
	return common.RedisHSetObj(
		getChannelQuotaLimitCacheKey(channelId),
		&cache,
		0,
	)
}

func setChannelQuotaLimitMissingCache(channelId int) {
	if err := setChannelQuotaLimitCache(channelId, false, false, 0); err != nil {
		common.SysLog(fmt.Sprintf("failed to cache missing channel quota limit: channel_id=%d, error=%v", channelId, err))
	}
}

func setChannelQuotaLimitsMissingCache(channelIds []int) {
	for _, channelId := range channelIds {
		setChannelQuotaLimitMissingCache(channelId)
	}
}

func updateChannelQuotaLimitCache(limit *ChannelQuotaLimit) {
	if limit == nil {
		return
	}
	if err := setChannelQuotaLimitCache(limit.ChannelId, true, limit.Enabled, limit.QuotaLimit); err != nil {
		common.SysLog(fmt.Sprintf("failed to cache channel quota limit: channel_id=%d, error=%v", limit.ChannelId, err))
	}
}

func GetChannelQuotaLimit(channelId int) (*ChannelQuotaLimit, error) {
	if channelId == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	if limit, err := getChannelQuotaLimitFromCache(channelId); err == nil || errors.Is(err, gorm.ErrRecordNotFound) {
		return limit, err
	}
	limit := &ChannelQuotaLimit{}
	err := DB.Where("channel_id = ?", channelId).First(limit).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			setChannelQuotaLimitMissingCache(channelId)
		}
		return nil, err
	}
	updateChannelQuotaLimitCache(limit)
	return limit, nil
}

func ApplyChannelQuotaLimit(channel *Channel) {
	if channel == nil || channel.Id == 0 {
		return
	}
	limit, err := GetChannelQuotaLimit(channel.Id)
	if err != nil {
		channel.QuotaLimitEnabled = false
		channel.QuotaLimit = 0
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			common.SysLog(fmt.Sprintf("failed to load channel quota limit: channel_id=%d, error=%v", channel.Id, err))
		}
		return
	}
	channel.QuotaLimitEnabled = limit.Enabled
	channel.QuotaLimit = limit.QuotaLimit
}

func SaveChannelQuotaLimit(channelId int, enabled bool, quotaLimit int64) error {
	if err := saveChannelQuotaLimitTx(DB, channelId, enabled, quotaLimit); err != nil {
		return err
	}
	if !enabled || quotaLimit <= 0 {
		setChannelQuotaLimitMissingCache(channelId)
	} else {
		updateChannelQuotaLimitCache(&ChannelQuotaLimit{
			ChannelId:  channelId,
			Enabled:    true,
			QuotaLimit: quotaLimit,
		})
	}
	return nil
}

func saveChannelQuotaLimitTx(tx *gorm.DB, channelId int, enabled bool, quotaLimit int64) error {
	if channelId == 0 {
		return nil
	}
	if !enabled || quotaLimit <= 0 {
		return tx.Where("channel_id = ?", channelId).Delete(&ChannelQuotaLimit{}).Error
	}
	now := common.GetTimestamp()
	limit := ChannelQuotaLimit{
		ChannelId:   channelId,
		Enabled:     true,
		QuotaLimit:  quotaLimit,
		CreatedTime: now,
		UpdatedTime: now,
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "channel_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"enabled":      true,
			"quota_limit":  quotaLimit,
			"updated_time": now,
		}),
	}).Create(&limit).Error
}

func SaveChannelQuotaLimitForChannel(channel *Channel) error {
	if channel == nil {
		return nil
	}
	return SaveChannelQuotaLimit(channel.Id, channel.QuotaLimitEnabled, channel.QuotaLimit)
}

func saveChannelQuotaLimitForChannelTx(tx *gorm.DB, channel *Channel) error {
	if channel == nil {
		return nil
	}
	return saveChannelQuotaLimitTx(tx, channel.Id, channel.QuotaLimitEnabled, channel.QuotaLimit)
}

func PatchChannelQuotaLimit(channelId int, enabled *bool, quotaLimit *int64) error {
	if enabled == nil && quotaLimit == nil {
		return nil
	}
	nextEnabled := false
	nextQuotaLimit := int64(0)
	if current, err := GetChannelQuotaLimit(channelId); err == nil {
		nextEnabled = current.Enabled
		nextQuotaLimit = current.QuotaLimit
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if enabled != nil {
		nextEnabled = *enabled
	}
	if quotaLimit != nil {
		nextQuotaLimit = *quotaLimit
	}
	return SaveChannelQuotaLimit(channelId, nextEnabled, nextQuotaLimit)
}

func DeleteChannelQuotaLimits(channelIds []int) error {
	if len(channelIds) == 0 {
		return nil
	}
	if err := deleteChannelQuotaLimitsTx(DB, channelIds); err != nil {
		return err
	}
	setChannelQuotaLimitsMissingCache(channelIds)
	return nil
}

func deleteChannelQuotaLimitsTx(tx *gorm.DB, channelIds []int) error {
	if len(channelIds) == 0 {
		return nil
	}
	return tx.Where("channel_id in ?", channelIds).Delete(&ChannelQuotaLimit{}).Error
}

func checkAndDisableChannelByQuotaLimit(channelId int) {
	limit, err := GetChannelQuotaLimit(channelId)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			common.SysLog(fmt.Sprintf("failed to check channel quota limit: channel_id=%d, error=%v", channelId, err))
		}
		return
	}
	if !limit.Enabled || limit.QuotaLimit <= 0 {
		return
	}
	channel, err := GetChannelById(channelId, true)
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to check channel quota limit: channel_id=%d, error=%v", channelId, err))
		return
	}
	if channel.Status != common.ChannelStatusEnabled || channel.UsedQuota < limit.QuotaLimit {
		return
	}

	reason := fmt.Sprintf("渠道使用额度已达到上限：已用 %d / 上限 %d", channel.UsedQuota, limit.QuotaLimit)
	info := channel.GetOtherInfo()
	info["status_reason"] = reason
	info["status_time"] = common.GetTimestamp()
	channel.SetOtherInfo(info)
	channel.Status = common.ChannelStatusAutoDisabled
	if err := channel.SaveWithoutKey(); err != nil {
		common.SysLog(fmt.Sprintf("failed to disable channel by quota limit: channel_id=%d, error=%v", channelId, err))
		return
	}
	CacheUpdateChannelStatus(channelId, common.ChannelStatusAutoDisabled)
	if err := UpdateAbilityStatus(channelId, false); err != nil {
		common.SysLog(fmt.Sprintf("failed to disable channel abilities by quota limit: channel_id=%d, error=%v", channelId, err))
	}
	common.SysLog(fmt.Sprintf("channel #%d disabled by quota limit: %s", channelId, reason))
}
