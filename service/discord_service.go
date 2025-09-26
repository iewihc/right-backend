package service

import (
	"context"
	"fmt"
	"math/rand"
	"right-backend/model"
	"right-backend/service/interfaces"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog"
)

// DiscordService handles interactions with Discord.
type DiscordService struct {
	logger              zerolog.Logger
	session             *discordgo.Session
	orderService        *OrderService
	driverService       *DriverService
	chatService         *ChatService
	fcmService          interfaces.FCMService
	notificationService *NotificationService
}

// NewDiscordService creates and initializes a new DiscordService.
func NewDiscordService(logger zerolog.Logger, botToken string, orderService *OrderService) (*DiscordService, error) {
	dg, err := discordgo.New("Bot " + botToken)
	if err != nil {
		return nil, fmt.Errorf("error creating Discord session: %w", err)
	}

	service := &DiscordService{
		logger:       logger.With().Str("service", "discord").Logger(),
		session:      dg,
		orderService: orderService,
	}

	dg.AddHandler(service.messageCreate)
	dg.AddHandler(service.interactionCreate) // Add interaction handler
	dg.AddHandler(service.ready)             // Add ready handler for slash command registration

	dg.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsGuilds

	err = dg.Open()
	if err != nil {
		return nil, fmt.Errorf("error opening connection: %w", err)
	}

	logger.Info().Msg("Discord bot is now running")
	return service, nil
}

// ready 處理 Discord ready 事件
func (s *DiscordService) ready(_ *discordgo.Session, r *discordgo.Ready) {
	s.logger.Info().
		Str("username", r.User.Username).
		Str("user_id", r.User.ID).
		Int("guild_count", len(r.Guilds)).
		Msg("Discord bot ready")

	// 列出所有連接的 Guild
	for _, guild := range r.Guilds {
		s.logger.Info().
			Str("guild_id", guild.ID).
			Str("guild_name", guild.Name).
			Msg("Connected to guild")
	}
}

// SetOrderService allows for delayed injection of the OrderService to break circular dependencies.
func (s *DiscordService) SetOrderService(orderService *OrderService) {
	s.orderService = orderService
	// 在 orderService 設置完成後註冊 slash commands
	// 需要等待 Discord 連接準備完成
	go func() {
		// 等待一秒讓 Discord 連接穩定
		time.Sleep(1 * time.Second)
		s.registerSlashCommands()
	}()
}

// SetDriverService allows for delayed injection of the DriverService to break circular dependencies.
func (s *DiscordService) SetDriverService(driverService *DriverService) {
	s.driverService = driverService
}

// registerSlashCommands 註冊 Discord slash commands
func (s *DiscordService) registerSlashCommands() {
	// 等待連接準備就緒
	if s.session.State.User == nil {
		s.logger.Warn().Msg("Discord session 未準備就緒，延後註冊 slash commands")
		return
	}

	// 清理現有的 slash commands（避免重複）
	s.cleanupOldCommands()

	commands := []*discordgo.ApplicationCommand{
		{
			Name:        string(model.SlashCommandPing),
			Description: "測試機器人連接狀態",
		},
		{
			Name:        string(model.SlashCommandResetDriver),
			Description: "將司機狀態強制重置為閒置並清除預約單跟當前訂單以及司機狀態",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "driver_identifier",
					Description: "司機識別資訊（可輸入：司機名稱、司機account、或driverNo司機編號）",
					Required:    true,
				},
			},
		},
		{
			Name:        string(model.SlashCommandCleanFailedOrders),
			Description: "根據車隊刪除所有狀態為流單的訂單",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "fleet",
					Description: "選擇要清理流單的車隊",
					Required:    true,
					Choices: []*discordgo.ApplicationCommandOptionChoice{
						{
							Name:  "RSK",
							Value: "RSK",
						},
						{
							Name:  "KD",
							Value: "KD",
						},
						{
							Name:  "WEI",
							Value: "WEI",
						},
					},
				},
			},
		},
		{
			Name:        string(model.SlashCommandSearchScheduled),
			Description: "查詢預約單情況",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "type",
					Description: "查詢類型",
					Required:    true,
					Choices: []*discordgo.ApplicationCommandOptionChoice{
						{
							Name:  "已分配的預約單",
							Value: "assigned",
						},
						{
							Name:  "未分配的預約單",
							Value: "unassigned",
						},
					},
				},
			},
		},
		{
			Name:        string(model.SlashCommandSearchOnlineDrivers),
			Description: "查詢所有在線司機列表",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "fleet",
					Description: "篩選特定車隊的司機（可選）",
					Required:    false,
					Choices: []*discordgo.ApplicationCommandOptionChoice{
						{
							Name:  "RSK",
							Value: "RSK",
						},
						{
							Name:  "KD",
							Value: "KD",
						},
						{
							Name:  "WEI",
							Value: "WEI",
						},
					},
				},
			},
		},
		{
			Name:        string(model.SlashCommandWeiEmptyOrderAndDriver),
			Description: "清空WEI車隊所有訂單並重置司機狀態",
		},
		{
			Name:        string(model.SlashCommandWeiCreateExampleOrder),
			Description: "為WEI車隊建立測試訂單",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "type",
					Description: "訂單類型",
					Required:    false,
					Choices: []*discordgo.ApplicationCommandOptionChoice{
						{
							Name:  "即時單",
							Value: "instant",
						},
						{
							Name:  "預約單",
							Value: "scheduled",
						},
					},
				},
			},
		},
	}

	// 獲取所有 guild 並為每個 guild 註冊指令
	guilds := s.session.State.Guilds
	if len(guilds) == 0 {
		s.logger.Warn().Msg("沒有找到 guild，註冊全域指令")
		// 註冊全域指令（需要 1 小時生效）
		for _, command := range commands {
			createdCommand, err := s.session.ApplicationCommandCreate(s.session.State.User.ID, "", command)
			if err != nil {
				s.logger.Error().Err(err).Str("command", command.Name).Msg("無法註冊全域 slash command")
			} else {
				s.logger.Info().
					Str("command", command.Name).
					Str("command_id", createdCommand.ID).
					Msg("成功註冊全域 slash command")
			}
		}
	} else {
		// 為每個 guild 註冊指令（立即生效）
		for _, guild := range guilds {
			s.logger.Info().
				Str("guild_id", guild.ID).
				Str("guild_name", guild.Name).
				Msg("為 guild 註冊 slash commands")

			for _, command := range commands {
				createdCommand, err := s.session.ApplicationCommandCreate(s.session.State.User.ID, guild.ID, command)
				if err != nil {
					s.logger.Error().
						Err(err).
						Str("command", command.Name).
						Str("guild_id", guild.ID).
						Msg("無法註冊 guild slash command")
				} else {
					s.logger.Info().
						Str("command", command.Name).
						Str("command_id", createdCommand.ID).
						Str("guild_id", guild.ID).
						Msg("成功註冊 guild slash command")
				}
			}
		}
	}
}

// cleanupOldCommands 清理舊的 slash commands
func (s *DiscordService) cleanupOldCommands() {
	// 清理全域指令
	s.cleanupCommandsForGuild("")

	// 清理所有 guild 的指令
	guilds := s.session.State.Guilds
	for _, guild := range guilds {
		s.cleanupCommandsForGuild(guild.ID)
	}
}

// cleanupCommandsForGuild 清理特定 guild 的指令
func (s *DiscordService) cleanupCommandsForGuild(guildID string) {
	commands, err := s.session.ApplicationCommands(s.session.State.User.ID, guildID)
	if err != nil {
		guildLabel := "全域"
		if guildID != "" {
			guildLabel = guildID
		}
		s.logger.Warn().
			Err(err).
			Str("guild", guildLabel).
			Msg("無法獲取現有的 slash commands")
		return
	}

	for _, command := range commands {
		if command.Name == string(model.SlashCommandCleanFailedOrders) ||
			command.Name == string(model.SlashCommandPing) ||
			command.Name == string(model.SlashCommandResetDriver) ||
			command.Name == string(model.SlashCommandSearchScheduled) ||
			command.Name == string(model.SlashCommandSearchOnlineDrivers) ||
			command.Name == string(model.SlashCommandWeiEmptyOrderAndDriver) ||
			command.Name == string(model.SlashCommandWeiCreateExampleOrder) {
			err := s.session.ApplicationCommandDelete(s.session.State.User.ID, guildID, command.ID)
			if err != nil {
				s.logger.Warn().
					Err(err).
					Str("command", command.Name).
					Str("guild_id", guildID).
					Msg("無法刪除舊的 slash command")
			} else {
				s.logger.Info().
					Str("command", command.Name).
					Str("guild_id", guildID).
					Msg("已清理舊的 slash command")
			}
		}
	}
}

// SetChatService allows for delayed injection of the ChatService to break circular dependencies.
func (s *DiscordService) SetChatService(chatService *ChatService) {
	s.chatService = chatService
}

// SetFCMService allows for delayed injection of the FCMService.
func (s *DiscordService) SetFCMService(fcmService interfaces.FCMService) {
	s.fcmService = fcmService
}

func (s *DiscordService) SetNotificationService(notificationService *NotificationService) {
	s.notificationService = notificationService
}

// SendMessage sends a message to a specific Discord channel.
func (s *DiscordService) SendMessage(channelID, message string) (*discordgo.Message, error) {
	return s.session.ChannelMessageSend(channelID, message)
}

// ReplyToMessage sends a reply to a specific message in a Discord channel.
func (s *DiscordService) ReplyToMessage(channelID, messageID, replyText string) (*discordgo.Message, error) {
	return s.session.ChannelMessageSendReply(channelID, replyText, &discordgo.MessageReference{
		MessageID: messageID,
		ChannelID: channelID,
	})
}

// ReplyToMessageWithOrderID 發送帶OrderID嵌入的文字回覆（簡潔方案）
func (s *DiscordService) ReplyToMessageWithOrderID(channelID, messageID, replyText, orderID string) (*discordgo.Message, error) {
	return s.ReplyToMessageWithOrderIDAndColor(channelID, messageID, replyText, orderID, 0x2F3136)
}

// ReplyToMessageWithOrderIDAndColor 發送帶OrderID和自定義顏色的文字回覆
func (s *DiscordService) ReplyToMessageWithOrderIDAndColor(channelID, messageID, replyText, orderID string, color int) (*discordgo.Message, error) {
	embed := &discordgo.MessageEmbed{
		Description: replyText,
		Footer: &discordgo.MessageEmbedFooter{
			Text: orderID, // 直接顯示orderID，Discord會自動以小字體顯示在左下角
		},
		Color: color, // 自定義顏色
	}

	return s.session.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{embed},
		Reference: &discordgo.MessageReference{
			MessageID: messageID,
			ChannelID: channelID,
		},
	})
}

// SendImageReplyWithOrderID 發送帶OrderID嵌入的圖片回覆（簡潔方案）
func (s *DiscordService) SendImageReplyWithOrderID(channelID, messageID, replyText, imageURL, orderID string) (*discordgo.Message, error) {
	return s.SendImageReplyWithOrderIDAndColor(channelID, messageID, replyText, imageURL, orderID, 0x2F3136)
}

// SendImageReplyWithOrderIDAndColor 發送帶OrderID和自定義顏色的圖片回覆
func (s *DiscordService) SendImageReplyWithOrderIDAndColor(channelID, messageID, replyText, imageURL, orderID string, color int) (*discordgo.Message, error) {
	embed := &discordgo.MessageEmbed{
		Description: replyText,
		Image: &discordgo.MessageEmbedImage{
			URL: imageURL,
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: orderID, // 直接顯示orderID，Discord會自動以小字體顯示在左下角
		},
		Color: color, // 自定義顏色
	}

	return s.session.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{embed},
		Reference: &discordgo.MessageReference{
			MessageID: messageID,
			ChannelID: channelID,
		},
	})
}

// SendImageReplyToMessage sends a reply with an image to a specific message in a Discord channel.
func (s *DiscordService) SendImageReplyToMessage(channelID, messageID, replyText, imageURL string) (*discordgo.Message, error) {
	s.logger.Info().
		Str("channel_id", channelID).
		Str("message_id", messageID).
		Str("image_url", imageURL).
		Msg("發送包含圖片的Discord回覆")

	// 檢查圖片 URL 的有效性
	if imageURL == "" {
		s.logger.Warn().Msg("圖片 URL 為空，回退到文字回覆")
		return s.ReplyToMessage(channelID, messageID, replyText)
	}

	// 方法1: 使用 embed 隱藏 URL 只顯示圖片
	embed := &discordgo.MessageEmbed{
		Description: replyText,
		Image: &discordgo.MessageEmbedImage{
			URL: imageURL,
		},
		Color: 0x3498DB, // 藍色
	}

	msg, err := s.session.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{embed},
		Reference: &discordgo.MessageReference{
			MessageID: messageID,
			ChannelID: channelID,
		},
	})

	if err != nil {
		s.logger.Error().Err(err).
			Str("image_url", imageURL).
			Msg("使用 embed 發送圖片失敗，回退到顯示 URL")

		// 方法2: 如果 embed 失敗，顯示 URL 作為回退
		fallbackText := fmt.Sprintf("%s\n%s", replyText, imageURL)
		return s.ReplyToMessage(channelID, messageID, fallbackText)
	}

	s.logger.Info().
		Str("image_url", imageURL).
		Msg("已使用 embed 發送 Discord 圖片回覆（不顯示 URL）")

	return msg, nil
}

// EditMessage edits an existing message in a Discord channel without components.
func (s *DiscordService) EditMessage(channelID, messageID, newMessage string) (*discordgo.Message, error) {
	return s.EditMessageWithComponents(channelID, messageID, newMessage, nil)
}

// EditMessageWithComponents edits an existing message and allows adding components like buttons.
func (s *DiscordService) EditMessageWithComponents(channelID, messageID, newContent string, components []discordgo.MessageComponent) (*discordgo.Message, error) {
	edit := &discordgo.MessageEdit{
		Content:    &newContent,
		Components: &components,
		Channel:    channelID,
		ID:         messageID,
	}
	return s.session.ChannelMessageEditComplex(edit)
}

// UpdateOrderCard formats and updates a Discord message based on order status.
func (s *DiscordService) UpdateOrderCard(order *model.Order) {
	if order.DiscordMessageID == "" || order.DiscordChannelID == "" {
		return
	}

	shortID := order.ShortID
	embed := &discordgo.MessageEmbed{
		Type: discordgo.EmbedTypeRich,
		Footer: &discordgo.MessageEmbedFooter{
			Text: shortID,
		},
		Timestamp: time.Now().Format(time.RFC3339), // Add timestamp
	}

	var components []discordgo.MessageComponent

	switch order.Status {
	case model.OrderStatusScheduleAccepted:
		// 預約單已被接受（尚未激活）
		embed.Title = fmt.Sprintf("✅ 預約單已被接受 (%s)", shortID)
		embed.Color = 0xF1C40F // Yellow for scheduled orders

		fields := []*discordgo.MessageEmbedField{
			{Name: "客戶群組", Value: order.CustomerGroup, Inline: true},
			{Name: "狀態", Value: "預約單已被接受", Inline: true},
			{Name: "上車地點", Value: order.OriText, Inline: false},
		}

		// 添加預約單欄位
		if scheduledField := createScheduledOrderField(order); scheduledField != nil {
			fields = append(fields, scheduledField)
		}
		// 添加 Google 地址（如果存在）
		if order.Customer.PickupAddress != "" {
			fields = append(fields, &discordgo.MessageEmbedField{
				Name: "Google地址", Value: order.Customer.PickupAddress, Inline: false,
			})
		}

		// 添加司機和預約時間資訊
		fields = append(fields, &discordgo.MessageEmbedField{
			Name: "備註", Value: order.Customer.Remarks, Inline: false,
		})
		fields = append(fields, &discordgo.MessageEmbedField{
			Name: "駕駛", Value: formatDriverInfo(order.Driver.Name, order.Driver.CarNo, order.Driver.CarColor), Inline: true,
		})

		// 顯示預約時間
		if order.ScheduledAt != nil {
			taipeiLocation := time.FixedZone("Asia/Taipei", 8*3600)
			scheduledTime := order.ScheduledAt.In(taipeiLocation).Format("01/02 15:04")
			fields = append(fields, &discordgo.MessageEmbedField{
				Name: "預約時間", Value: scheduledTime, Inline: true,
			})
		}

		embed.Fields = fields

		// 添加司機上傳的到達證明照片
		if order.PickupCertificateURL != "" {
			embed.Image = &discordgo.MessageEmbedImage{
				URL: order.PickupCertificateURL,
			}
		}

	case model.OrderStatusEnroute:
		// 特別處理預約單的情況
		if order.Type == model.OrderTypeScheduled {
			embed.Title = fmt.Sprintf("🚗 司機前往上車點 (%s)", shortID)
			embed.Color = 0xF1C40F // Yellow for scheduled orders
		} else {
			embed.Title = fmt.Sprintf("🚗 司機前往上車點 (%s)", shortID)
			embed.Color = 0x2ECC71 // Green for instant orders
		}

		displayMins := order.Driver.EstPickupMins
		if order.Driver.AdjustMins != nil {
			displayMins += *order.Driver.AdjustMins
		}

		// 計算預計到達的具體時間（台北時間）
		taipeiLocation := time.FixedZone("Asia/Taipei", 8*3600)
		arrivalTime := time.Now().In(taipeiLocation).Add(time.Minute * time.Duration(displayMins))
		arrivalTimeFormatted := arrivalTime.Format("15:04")

		fields := []*discordgo.MessageEmbedField{
			{Name: "客戶群組", Value: order.CustomerGroup, Inline: true},
		}

		// 為預約單和即時單顯示不同的狀態
		if order.Type == model.OrderTypeScheduled {
			fields = append(fields, &discordgo.MessageEmbedField{
				Name: "狀態", Value: "預約單已被接受", Inline: true,
			})
		} else {
			fields = append(fields, &discordgo.MessageEmbedField{
				Name: "狀態", Value: string(order.Status), Inline: true,
			})
		}

		fields = append(fields, &discordgo.MessageEmbedField{
			Name: "上車地點", Value: order.OriText, Inline: false,
		})
		// 添加預約單欄位（如果是預約單）
		if scheduledField := createScheduledOrderField(order); scheduledField != nil {
			fields = append(fields, scheduledField)
		}
		// 添加 Google 地址（如果存在）
		if order.Customer.PickupAddress != "" {
			fields = append(fields, &discordgo.MessageEmbedField{
				Name: "Google地址", Value: order.Customer.PickupAddress, Inline: false,
			})
		}
		// 添加共同欄位
		fields = append(fields, &discordgo.MessageEmbedField{
			Name: "備註", Value: order.Customer.Remarks, Inline: false,
		})
		fields = append(fields, &discordgo.MessageEmbedField{
			Name: "駕駛", Value: formatDriverInfo(order.Driver.Name, order.Driver.CarNo, order.Driver.CarColor), Inline: true,
		})

		// 為預約單和即時單顯示不同的時間資訊
		if order.Type == model.OrderTypeScheduled {
			// 預約單顯示預約時間而不是預計到達時間
			if order.ScheduledAt != nil {
				scheduledTime := order.ScheduledAt.In(taipeiLocation).Format("01/02 15:04")
				fields = append(fields, &discordgo.MessageEmbedField{
					Name: "預約時間", Value: scheduledTime, Inline: true,
				})
			}
		} else {
			// 即時單顯示預計到達時間和調整資訊
			timeInfo := fmt.Sprintf("%d 分鐘 (%s)", displayMins, arrivalTimeFormatted)
			
			// 如果有司機調整時間，額外顯示原始時間和調整信息
			if order.Driver.AdjustMins != nil && *order.Driver.AdjustMins != 0 {
				originalMins := order.Driver.EstPickupMins
				adjustMins := *order.Driver.AdjustMins
				timeInfo += fmt.Sprintf("\n📝 原始: %d分 | 調整: %+d分", originalMins, adjustMins)
			}
			
			fields = append(fields, &discordgo.MessageEmbedField{
				Name: "預計到達", Value: timeInfo, Inline: true,
			})
		}
		embed.Fields = fields

		// 添加司機上傳的到達證明照片
		if order.PickupCertificateURL != "" {
			embed.Image = &discordgo.MessageEmbedImage{
				URL: order.PickupCertificateURL,
			}
		}

		// 添加取消按鈕
		components = []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{
						Label:    "取消訂單",
						Style:    discordgo.SecondaryButton,
						CustomID: "cancel_" + order.ID.Hex(),
						Emoji:    &discordgo.ComponentEmoji{Name: "❌"},
					},
				},
			},
		}

	case model.OrderStatusExecuting:
		embed.Title = fmt.Sprintf("🚗 乘客已上車 (%s)", shortID)
		embed.Color = 0x3498DB // Blue
		fields := []*discordgo.MessageEmbedField{
			{Name: "客戶群組", Value: order.CustomerGroup, Inline: true},
			{Name: "狀態", Value: string(order.Status), Inline: true},
			{Name: "上車地點", Value: order.OriText, Inline: false},
		}
		// 添加預約單欄位（如果是預約單）
		if scheduledField := createScheduledOrderField(order); scheduledField != nil {
			fields = append(fields, scheduledField)
		}
		// 添加 Google 地址（如果存在）
		if order.Customer.PickupAddress != "" {
			fields = append(fields, &discordgo.MessageEmbedField{
				Name: "Google地址", Value: order.Customer.PickupAddress, Inline: false,
			})
		}
		fields = append(fields, []*discordgo.MessageEmbedField{
			{Name: "備註", Value: order.Customer.Remarks, Inline: false},
			{Name: "駕駛", Value: formatDriverInfo(order.Driver.Name, order.Driver.CarNo, order.Driver.CarColor), Inline: true},
		}...)

		// 添加早到/遲到資訊
		if arrivalInfo := formatArrivalDeviation(order.Driver.ArrivalDeviationSecs); arrivalInfo != "" {
			fields = append(fields, &discordgo.MessageEmbedField{
				Name: "抵達狀況", Value: arrivalInfo, Inline: true,
			})
		}

		embed.Fields = fields

		// 添加司機上傳的到達證明照片
		if order.PickupCertificateURL != "" {
			embed.Image = &discordgo.MessageEmbedImage{
				URL: order.PickupCertificateURL,
			}
		}

	case model.OrderStatusFailed:
		embed.Title = fmt.Sprintf("❌ 派單失敗 (%s)", shortID)
		embed.Color = 0xE74C3C // Red
		fields := []*discordgo.MessageEmbedField{
			{Name: "客戶群組", Value: order.CustomerGroup, Inline: true},
			{Name: "狀態", Value: string(order.Status), Inline: true},
			{Name: "上車地點", Value: order.OriText, Inline: false},
		}
		// 添加預約單欄位（如果是預約單）
		if scheduledField := createScheduledOrderField(order); scheduledField != nil {
			fields = append(fields, scheduledField)
		}
		// 添加 Google 地址（如果存在）
		if order.Customer.PickupAddress != "" {
			fields = append(fields, &discordgo.MessageEmbedField{
				Name: "Google地址", Value: order.Customer.PickupAddress, Inline: false,
			})
		}
		fields = append(fields, []*discordgo.MessageEmbedField{
			{Name: "備註", Value: order.Customer.Remarks, Inline: false},
			{Name: "原因", Value: "很抱歉，目前沒有可用的司機。", Inline: false},
		}...)
		embed.Fields = fields
		components = []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{
						Label:    "重新派單",
						Style:    discordgo.PrimaryButton,
						CustomID: "redispatch_" + order.ID.Hex(),
					},
				},
			},
		}

	case model.OrderStatusDriverArrived:
		embed.Title = fmt.Sprintf("📍 司機到達客上位置 (%s)", shortID)
		embed.Color = 0xE67E22 // Orange
		fields := []*discordgo.MessageEmbedField{
			{Name: "客戶群組", Value: order.CustomerGroup, Inline: true},
			{Name: "狀態", Value: "調度請通知乘客", Inline: true},
			{Name: "上車地點", Value: order.OriText, Inline: false},
		}
		// 添加預約單欄位（如果是預約單）
		if scheduledField := createScheduledOrderField(order); scheduledField != nil {
			fields = append(fields, scheduledField)
		}
		// 添加 Google 地址（如果存在）
		if order.Customer.PickupAddress != "" {
			fields = append(fields, &discordgo.MessageEmbedField{
				Name: "Google地址", Value: order.Customer.PickupAddress, Inline: false,
			})
		}
		fields = append(fields, []*discordgo.MessageEmbedField{
			{Name: "備註", Value: order.Customer.Remarks, Inline: false},
			{Name: "駕駛", Value: formatDriverInfo(order.Driver.Name, order.Driver.CarNo, order.Driver.CarColor), Inline: true},
		}...)

		// 添加早到/遲到資訊欄位
		if arrivalInfo := formatArrivalDeviation(order.Driver.ArrivalDeviationSecs); arrivalInfo != "" {
			fields = append(fields, &discordgo.MessageEmbedField{
				Name: "抵達狀況", Value: arrivalInfo, Inline: true,
			})
		}

		embed.Fields = fields
		if order.PickupCertificateURL != "" {
			embed.Image = &discordgo.MessageEmbedImage{
				URL: order.PickupCertificateURL,
			}
		}

		// 添加取消按鈕
		components = []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{
						Label:    "取消訂單",
						Style:    discordgo.SecondaryButton,
						CustomID: "cancel_" + order.ID.Hex(),
						Emoji:    &discordgo.ComponentEmoji{Name: "❌"},
					},
				},
			},
		}

	case model.OrderStatusCompleted:
		embed.Title = fmt.Sprintf("🏁 訂單已完成 (%s)", shortID)
		embed.Color = 0x57F287 // Discord Success Green
		finalFields := []*discordgo.MessageEmbedField{
			{Name: "客戶群組", Value: order.CustomerGroup, Inline: true},
			{Name: "狀態", Value: "已完成", Inline: true},
			{Name: "上車地點", Value: order.OriText, Inline: false},
		}
		// 添加預約單欄位（如果是預約單）
		if scheduledField := createScheduledOrderField(order); scheduledField != nil {
			finalFields = append(finalFields, scheduledField)
		}
		// 添加 Google 地址（如果存在）
		if order.Customer.PickupAddress != "" {
			finalFields = append(finalFields, &discordgo.MessageEmbedField{
				Name: "Google地址", Value: order.Customer.PickupAddress, Inline: false,
			})
		}
		finalFields = append(finalFields, []*discordgo.MessageEmbedField{
			{Name: "備註", Value: order.Customer.Remarks, Inline: false},
			{Name: "駕駛", Value: formatDriverInfo(order.Driver.Name, order.Driver.CarNo, order.Driver.CarColor), Inline: true},
		}...)

		// 添加早到/遲到資訊
		if arrivalInfo := formatArrivalDeviation(order.Driver.ArrivalDeviationSecs); arrivalInfo != "" {
			finalFields = append(finalFields, &discordgo.MessageEmbedField{
				Name: "抵達狀況", Value: arrivalInfo, Inline: true,
			})
		}

		if order.Amount != nil {
			finalFields = append(finalFields, &discordgo.MessageEmbedField{Name: "車資", Value: fmt.Sprintf("$%d", *order.Amount), Inline: true})
		}
		embed.Fields = finalFields

		// 添加司機上傳的到達證明照片
		if order.PickupCertificateURL != "" {
			embed.Image = &discordgo.MessageEmbedImage{
				URL: order.PickupCertificateURL,
			}
		}

	default: // Searching, Cancelled etc.
		// 特別處理等待接單狀態
		if order.Status == model.OrderStatusWaiting && order.Type == model.OrderTypeScheduled {
			embed.Title = fmt.Sprintf("⏳ 等待接單－預約單 (%s)", shortID)
			embed.Color = 0xF1C40F // Yellow for scheduled orders
		} else if order.Status == model.OrderStatusWaiting && order.Type == model.OrderTypeInstant {
			// 判斷是否為預約單轉換而來的即時單
			if order.ConvertedFrom == "scheduled" {
				embed.Title = fmt.Sprintf("🔄 轉換即時單－等待接單 (%s)", shortID)
				embed.Color = 0xFF6B6B // Red for converted instant orders
			} else {
				embed.Title = fmt.Sprintf("⏳ 等待接單－即時單 (%s)", shortID)
				embed.Color = 0x3498DB // Blue for regular instant orders
			}
		} else {
			embed.Title = fmt.Sprintf("⏳ %s (%s)", string(order.Status), shortID)
			embed.Color = 0x95A5A6 // Grey
		}
		defaultFields := []*discordgo.MessageEmbedField{
			{Name: "客戶群組", Value: order.CustomerGroup, Inline: true},
			{Name: "狀態", Value: string(order.Status), Inline: true},
			{Name: "上車地點", Value: order.OriText, Inline: false},
		}
		// 添加預約單欄位（如果是預約單）
		if scheduledField := createScheduledOrderField(order); scheduledField != nil {
			defaultFields = append(defaultFields, scheduledField)
		}
		// 添加 Google 地址（如果存在）
		if order.Customer.PickupAddress != "" {
			defaultFields = append(defaultFields, &discordgo.MessageEmbedField{
				Name: "Google地址", Value: order.Customer.PickupAddress, Inline: false,
			})
		}
		defaultFields = append(defaultFields, &discordgo.MessageEmbedField{
			Name: "備註", Value: order.Customer.Remarks, Inline: false,
		})
		embed.Fields = defaultFields

		// 添加司機上傳的到達證明照片
		if order.PickupCertificateURL != "" {
			embed.Image = &discordgo.MessageEmbedImage{
				URL: order.PickupCertificateURL,
			}
		}

		// 為可取消的狀態添加取消按鈕（如 OrderStatusWaiting）
		if order.Status == model.OrderStatusWaiting {
			components = []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.Button{
							Label:    "取消訂單",
							Style:    discordgo.SecondaryButton,
							CustomID: "cancel_" + order.ID.Hex(),
							Emoji:    &discordgo.ComponentEmoji{Name: "❌"},
						},
					},
				},
			}
		}
	}

	// 明确设置 Content 为空字符串指针，以清除旧的文字内容
	emptyContent := ""
	edit := &discordgo.MessageEdit{
		Channel:    order.DiscordChannelID,
		ID:         order.DiscordMessageID,
		Content:    &emptyContent,
		Embeds:     &[]*discordgo.MessageEmbed{embed},
		Components: &components,
	}

	_, err := s.session.ChannelMessageEditComplex(edit)
	if err != nil {
		s.logger.Error().
			Err(err).
			Str("discord_message_id", order.DiscordMessageID).
			Str("discord_channel_id", order.DiscordChannelID).
			Msg("Failed to edit discord message with embed")
	}
}

func (s *DiscordService) interactionCreate(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	// 記錄所有收到的 interaction
	s.logger.Info().
		Int("interaction_type", int(i.Type)).
		Str("interaction_id", i.ID).
		Msg("收到 Discord interaction")

	// Handle slash commands
	if i.Type == discordgo.InteractionApplicationCommand {
		s.logger.Info().
			Str("command", i.ApplicationCommandData().Name).
			Msg("處理 slash command")
		s.handleSlashCommand(sess, i)
		return
	}

	// Handle button clicks (Message Components)
	if i.Type != discordgo.InteractionMessageComponent {
		return
	}

	customID := i.MessageComponentData().CustomID
	s.logger.Info().
		Str("custom_id", customID).
		Msg("Received discord interaction")

	if strings.HasPrefix(customID, "redispatch_") {
		// Acknowledge the button press to prevent "Interaction failed" and remove the button
		err := sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    i.Message.Content + "\n\n`收到！正在為您重新派單...`",
				Components: []discordgo.MessageComponent{}, // Empty components removes the button
			},
		})
		if err != nil {
			s.logger.Error().
				Err(err).
				Msg("Error responding to discord interaction")
			return
		}

		go s.handleRedispatch(i)
	} else if strings.HasPrefix(customID, "cancel_") {
		// 處理取消按鈕點擊
		err := sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    i.Message.Content + "\n\n`收到！正在為您取消訂單...`",
				Components: []discordgo.MessageComponent{}, // 移除所有按鈕
			},
		})
		if err != nil {
			s.logger.Error().
				Err(err).
				Msg("Error responding to discord cancel interaction")
			return
		}

		go s.handleCancelInteraction(i)
	}
}

// handleSlashCommand 處理 slash commands
func (s *DiscordService) handleSlashCommand(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	// 添加 panic 恢復
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error().
				Interface("panic", r).
				Msg("handleSlashCommand 發生 panic")

			// 嘗試回應錯誤
			err := sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "❌ 指令處理時發生錯誤，請稍後再試",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			if err != nil {
				s.logger.Error().Err(err).Msg("回應 panic 錯誤失敗")
			}
		}
	}()

	commandName := i.ApplicationCommandData().Name

	// 獲取用戶名稱
	userName := "Discord用戶"
	if i.Member != nil && i.Member.User != nil {
		userName = i.Member.User.Username
	} else if i.User != nil {
		userName = i.User.Username
	}

	s.logger.Info().
		Str("command", commandName).
		Str("user", userName).
		Msg("收到 slash command")

	switch commandName {
	case string(model.SlashCommandPing):
		s.handlePingCommand(sess, i)
	case string(model.SlashCommandResetDriver):
		s.handleResetDriverCommand(sess, i)
	case string(model.SlashCommandCleanFailedOrders):
		s.handleCleanFailedOrdersCommand(sess, i)
	case string(model.SlashCommandSearchScheduled):
		s.handleScheduledOrdersCommand(sess, i)
	case string(model.SlashCommandSearchOnlineDrivers):
		s.handleOnlineDriversCommand(sess, i)
	case string(model.SlashCommandWeiEmptyOrderAndDriver):
		s.handleWeiEmptyOrderAndDriverCommand(sess, i)
	case string(model.SlashCommandWeiCreateExampleOrder):
		s.handleWeiCreateExampleOrderCommand(sess, i)
	default:
		s.logger.Warn().Str("command", commandName).Msg("未知的 slash command")
		err := sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ 未知的指令",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		if err != nil {
			s.logger.Error().Err(err).Msg("回應未知指令失敗")
		}
	}
}

// handlePingCommand 處理 ping 指令
func (s *DiscordService) handlePingCommand(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	s.logger.Info().Msg("處理 ping 指令")

	err := sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "🏓 Pong! 機器人連接正常",
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		s.logger.Error().Err(err).Msg("回應 ping 指令失敗")
	} else {
		s.logger.Info().Msg("成功回應 ping 指令")
	}
}

// handleResetDriverCommand 處理重置司機狀態指令
func (s *DiscordService) handleResetDriverCommand(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	// 記錄收到指令的詳細資訊
	s.logger.Info().
		Str("command", string(model.SlashCommandResetDriver)).
		Msg("開始處理重置司機狀態指令")

	// 獲取用戶名稱
	userName := "Discord用戶"
	if i.Member != nil && i.Member.User != nil {
		userName = i.Member.User.Username
	} else if i.User != nil {
		userName = i.User.Username
	}

	s.logger.Info().
		Str("user", userName).
		Msg("用戶資訊已獲取")

	if s.orderService == nil {
		s.logger.Error().Msg("OrderService 未初始化")
		err := sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ 服務未就緒，請稍後再試",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		if err != nil {
			s.logger.Error().Err(err).Msg("回應 Discord 交互失敗")
		}
		return
	}

	s.logger.Info().Msg("OrderService 已確認初始化")

	// 獲取司機識別資訊參數
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		err := sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ 請輸入司機識別資訊（司機名稱、account或driverNo）",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		if err != nil {
			s.logger.Error().Err(err).Msg("回應司機識別資訊錯誤失敗")
		}
		return
	}

	driverIdentifier := options[0].StringValue()
	if driverIdentifier == "" {
		err := sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ 司機識別資訊不能為空",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		if err != nil {
			s.logger.Error().Err(err).Msg("回應司機識別資訊空值錯誤失敗")
		}
		return
	}

	s.logger.Info().
		Str("driver_identifier", driverIdentifier).
		Str("user", userName).
		Msg("處理重置司機狀態指令")

	// 先回應用戶，表示正在處理
	s.logger.Info().
		Str("driver_identifier", driverIdentifier).
		Msg("準備回應 Discord 指令")

	err := sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("⏳ 正在重置司機 %s 的狀態為閒置並清除預約單資訊...", driverIdentifier),
		},
	})
	if err != nil {
		s.logger.Error().
			Err(err).
			Str("driver_identifier", driverIdentifier).
			Str("user", userName).
			Msg("回應 slash command 失敗")
		return
	}

	s.logger.Info().
		Str("driver_identifier", driverIdentifier).
		Str("user", userName).
		Msg("已成功回應 Discord 指令，開始背景處理")

	// 在背景執行重置操作
	go s.processResetDriver(context.Background(), i, driverIdentifier, userName)
}

// processResetDriver 執行重置司機狀態的背景任務
func (s *DiscordService) processResetDriver(ctx context.Context, i *discordgo.InteractionCreate, driverIdentifier string, userName string) {
	s.logger.Info().
		Str("driver_identifier", driverIdentifier).
		Str("user", userName).
		Msg("開始重置司機狀態")

	// 檢查 DriverService 是否已初始化
	if s.driverService == nil {
		s.logger.Error().Msg("DriverService 未初始化")
		_, err := s.session.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "❌ DriverService 未就緒，請稍後再試",
		})
		if err != nil {
			s.logger.Error().Err(err).Msg("回應 DriverService 未初始化錯誤失敗")
		}
		return
	}

	// 執行重置操作（使用新的方法支援多種查詢方式並清除預約單資訊）
	resetDriver, err := s.driverService.ResetDriverWithScheduleClear(ctx, driverIdentifier, userName)
	if err != nil {
		s.logger.Error().Err(err).
			Str("driver_identifier", driverIdentifier).
			Str("user", userName).
			Msg("重置司機狀態失敗")

		// 發送失敗消息
		_, followupErr := s.session.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: fmt.Sprintf("❌ 重置司機失敗：%v", err),
		})
		if followupErr != nil {
			s.logger.Error().Err(followupErr).Msg("回應重置司機失敗錯誤失敗")
		}
		return
	}

	s.logger.Info().
		Str("driver_identifier", driverIdentifier).
		Str("driver_id", resetDriver.ID.Hex()).
		Str("driver_name", resetDriver.Name).
		Str("user", userName).
		Msg("重置司機狀態完成")

	// 構建詳細的成功消息
	var identifierType string
	if resetDriver.Name == driverIdentifier {
		identifierType = "司機名稱"
	} else if resetDriver.Account == driverIdentifier {
		identifierType = "司機帳號"
	} else if resetDriver.DriverNo == driverIdentifier {
		identifierType = "司機編號"
	} else {
		identifierType = "識別資訊"
	}

	successMessage := fmt.Sprintf("✅ **司機重置成功**\n"+
		"🔍 查詢方式：%s (%s)\n"+
		"👤 司機姓名：%s\n"+
		"📧 司機帳號：%s\n"+
		"🆔 司機編號：%s\n"+
		"📊 狀態：已重置為閒置\n"+
		"🗂️ 預約單資訊：已清除\n"+
		"👥 操作人員：%s",
		identifierType, driverIdentifier,
		resetDriver.Name,
		resetDriver.Account,
		resetDriver.DriverNo,
		userName)

	_, err = s.session.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: successMessage,
	})
	if err != nil {
		s.logger.Error().Err(err).Msg("回應重置司機成功訊息失敗")
	}
}

// handleCleanFailedOrdersCommand 處理清理流單指令
func (s *DiscordService) handleCleanFailedOrdersCommand(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	// 記錄收到指令的詳細資訊
	s.logger.Info().
		Str("command", string(model.SlashCommandCleanFailedOrders)).
		Msg("開始處理清理流單指令")

	// 獲取用戶名稱
	userName := "Discord用戶"
	if i.Member != nil && i.Member.User != nil {
		userName = i.Member.User.Username
	} else if i.User != nil {
		userName = i.User.Username
	}

	s.logger.Info().
		Str("user", userName).
		Msg("用戶資訊已獲取")

	if s.orderService == nil {
		s.logger.Error().Msg("OrderService 未初始化")
		err := sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ 服務未就緒，請稍後再試",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		if err != nil {
			s.logger.Error().Err(err).Msg("回應 Discord 交互失敗")
		}
		return
	}

	s.logger.Info().Msg("OrderService 已確認初始化")

	// 獲取車隊參數
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		err := sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ 請選擇車隊",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		if err != nil {
			s.logger.Error().Err(err).Msg("回應車隊選擇錯誤失敗")
		}
		return
	}

	fleetValue := options[0].StringValue()
	fleet := model.FleetType(fleetValue)

	s.logger.Info().
		Str("fleet", fleetValue).
		Str("user", userName).
		Msg("處理清理流單指令")

	// 驗證車隊
	if fleet != model.FleetTypeRSK && fleet != model.FleetTypeKD && fleet != model.FleetTypeWEI {
		err := sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ 無效的車隊選擇",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		if err != nil {
			s.logger.Error().Err(err).Msg("回應無效車隊錯誤失敗")
		}
		return
	}

	// 先回應用戶，表示正在處理
	s.logger.Info().
		Str("fleet", fleetValue).
		Msg("準備回應 Discord 指令")

	err := sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("⏳ 正在清理 %s 車隊的流單...", fleet),
		},
	})
	if err != nil {
		s.logger.Error().
			Err(err).
			Str("fleet", fleetValue).
			Str("user", userName).
			Msg("回應 slash command 失敗")
		return
	}

	s.logger.Info().
		Str("fleet", fleetValue).
		Str("user", userName).
		Msg("已成功回應 Discord 指令，開始背景處理")

	// 在背景執行刪除操作
	go s.processCleanFailedOrders(context.Background(), i, fleet, userName)
}

// processCleanFailedOrders 執行清理流單的背景任務
func (s *DiscordService) processCleanFailedOrders(ctx context.Context, i *discordgo.InteractionCreate, fleet model.FleetType, userName string) {
	s.logger.Info().
		Str("fleet", string(fleet)).
		Str("user", userName).
		Msg("開始清理車隊流單")

	// 執行刪除操作
	deletedCount, err := s.orderService.DeleteFailedOrdersByFleet(ctx, fleet)
	if err != nil {
		s.logger.Error().Err(err).
			Str("fleet", string(fleet)).
			Str("user", userName).
			Msg("清理車隊流單失敗")

		// 發送失敗消息
		_, followupErr := s.session.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: fmt.Sprintf("❌ 清理 %s 車隊流單失敗：%v", fleet, err),
		})
		if followupErr != nil {
			s.logger.Error().Err(followupErr).Msg("回應清理流單失敗錯誤失敗")
		}
		return
	}

	s.logger.Info().
		Str("fleet", string(fleet)).
		Str("user", userName).
		Int("deleted_count", deletedCount).
		Msg("清理車隊流單完成")

	// 發送成功消息
	var message string
	if deletedCount == 0 {
		message = fmt.Sprintf("✅ %s 車隊沒有需要清理的流單", fleet)
	} else {
		message = fmt.Sprintf("✅ 成功清理 %s 車隊的 %d 個流單", fleet, deletedCount)
	}

	_, err = s.session.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: message,
	})
	if err != nil {
		s.logger.Error().Err(err).Msg("回應清理流單結果失敗")
	}
}

// handleScheduledOrdersCommand 處理查詢預約單指令
func (s *DiscordService) handleScheduledOrdersCommand(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	s.logger.Info().
		Str("command", string(model.SlashCommandSearchScheduled)).
		Msg("處理查詢預約單指令")

	// 獲取查詢類型參數
	queryType := "assigned" // 預設值
	if len(i.ApplicationCommandData().Options) > 0 {
		for _, option := range i.ApplicationCommandData().Options {
			if option.Name == "type" {
				queryType = option.StringValue()
			}
		}
	}

	// 根據查詢類型設定提示訊息
	var message string
	switch queryType {
	case "assigned":
		message = "🔍 正在查詢所有已分配的預約單..."
	case "unassigned":
		message = "🔍 正在查詢所有未分配的預約單..."
	default:
		message = "🔍 正在查詢預約單..."
	}

	// 立即回應，表示正在處理
	err := sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: message,
		},
	})

	if err != nil {
		s.logger.Error().Err(err).Msg("回應查詢預約單指令失敗")
		return
	}

	// 獲取用戶名稱
	userName := "Discord用戶"
	if i.Member != nil && i.Member.User != nil {
		userName = i.Member.User.Username
	} else if i.User != nil {
		userName = i.User.Username
	}

	// 背景執行查詢
	go s.processScheduledOrdersQuery(context.Background(), i, userName, queryType)
}

// processScheduledOrdersQuery 執行查詢預約單的背景任務
func (s *DiscordService) processScheduledOrdersQuery(ctx context.Context, i *discordgo.InteractionCreate, userName string, queryType string) {
	s.logger.Info().
		Str("user", userName).
		Str("query_type", queryType).
		Msg("開始查詢預約單")

	// 檢查 orderService 是否可用
	if s.orderService == nil {
		s.logger.Error().Msg("orderService 未初始化")
		_, err := s.session.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "❌ 訂單服務未初始化，無法查詢預約單",
		})
		if err != nil {
			s.logger.Error().Err(err).Msg("回應查詢預約單服務未初始化錯誤失敗")
		}
		return
	}

	// 根據查詢類型調用對應的方法
	var orders []*model.Order
	var err error
	switch queryType {
	case "assigned":
		orders, err = s.orderService.GetAllAssignedScheduledOrders(ctx)
	case "unassigned":
		orders, err = s.orderService.GetUnassignedScheduledOrders(ctx)
	default:
		orders, err = s.orderService.GetAllAssignedScheduledOrders(ctx)
	}

	if err != nil {
		s.logger.Error().Err(err).Str("query_type", queryType).Msg("查詢預約單失敗")
		_, followupErr := s.session.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "❌ 查詢預約單失敗: " + err.Error(),
		})
		if followupErr != nil {
			s.logger.Error().Err(followupErr).Msg("回應查詢預約單失敗錯誤失敗")
		}
		return
	}

	// 構建回應訊息
	var message string
	if len(orders) == 0 {
		switch queryType {
		case "assigned":
			message = "📋 目前沒有已分配的預約單"
		case "unassigned":
			message = "📋 目前沒有未分配的預約單"
		default:
			message = "📋 目前沒有預約單"
		}
	} else {
		var title string
		switch queryType {
		case "assigned":
			title = "當前已分配的預約單"
		case "unassigned":
			title = "當前未分配的預約單"
		default:
			title = "當前預約單"
		}
		message = fmt.Sprintf("📋 **%s** (%d個)\n\n", title, len(orders))

		taipeiLocation := time.FixedZone("Asia/Taipei", 8*3600)

		for i, order := range orders {
			// 格式化預約時間
			scheduledTimeStr := "未設定"
			if order.ScheduledAt != nil {
				scheduledTimeStr = order.ScheduledAt.In(taipeiLocation).Format("01/02 15:04")
			}

			// 狀態圖標
			statusIcon := "⏳"
			switch order.Status {
			case model.OrderStatusWaiting:
				statusIcon = "⏳"
			case model.OrderStatusScheduleAccepted:
				statusIcon = "✅"
			case model.OrderStatusEnroute:
				statusIcon = "🚗"
			case model.OrderStatusDriverArrived:
				statusIcon = "📍"
			case model.OrderStatusExecuting:
				statusIcon = "🏃"
			}

			// 使用完整地址
			oriText := order.OriText

			if queryType == "assigned" {
				// 司機資訊
				driverName := "未知司機"
				if order.Driver.Name != "" {
					driverName = order.Driver.Name
				}

				// 車牌資訊
				carPlate := "無車牌"
				if order.Driver.CarNo != "" {
					carPlate = order.Driver.CarNo
				}

				// 單行格式：序號 圖標 訂單號 | 預約時間 | 地址 | 司機(車牌) | 狀態
				message += fmt.Sprintf("%d. %s %s | %s | %s | %s(%s) | %s\n",
					i+1, statusIcon, order.ShortID, scheduledTimeStr, oriText,
					driverName, carPlate, string(order.Status))
			} else {
				// 計算距離預約時間還有多久
				timeInfo := ""
				if order.ScheduledAt != nil {
					now := time.Now().In(taipeiLocation)
					timeUntil := order.ScheduledAt.In(taipeiLocation).Sub(now)
					if timeUntil > 0 {
						hours := int(timeUntil.Hours())
						minutes := int(timeUntil.Minutes()) % 60
						if hours > 0 {
							timeInfo = fmt.Sprintf("還有%dh%dm", hours, minutes)
						} else {
							timeInfo = fmt.Sprintf("還有%dm", minutes)
						}
					} else {
						timeInfo = "已逾時"
					}
				}

				// 單行格式：序號 圖標 訂單號 | 預約時間 | 地址 | 車隊 | 狀態 | 倒數時間
				message += fmt.Sprintf("%d. %s %s | %s | %s | %s車隊 | %s | %s\n",
					i+1, statusIcon, order.ShortID, scheduledTimeStr, oriText,
					string(order.Fleet), string(order.Status), timeInfo)
			}
		}

		// 如果訊息太長，截斷
		if len(message) > 1900 {
			message = message[:1900] + "...\n\n*(因訊息過長已截斷)*"
		}
	}

	// 發送結果
	_, err = s.session.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: message,
	})
	if err != nil {
		s.logger.Error().Err(err).Msg("回應預約單查詢結果失敗")
	}

	s.logger.Info().
		Str("user", userName).
		Int("orders_count", len(orders)).
		Msg("預約單查詢完成")
}

func (s *DiscordService) handleRedispatch(i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID
	originalOrderID := strings.TrimPrefix(customID, "redispatch_")

	_, err := s.orderService.RedispatchOrder(context.Background(), originalOrderID)
	if err != nil {
		s.logger.Error().
			Err(err).
			Str("order_id", originalOrderID).
			Msg("Failed to redispatch order")
		// Send a followup message to inform the user of the failure.
		_, followupErr := s.session.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: fmt.Sprintf("重新派單失敗，請聯繫管理員。錯誤: %v", err),
		})
		if followupErr != nil {
			s.logger.Error().Err(followupErr).Msg("回應重新派單失敗錯誤失敗")
		}
		return
	}

	s.logger.Info().
		Str("order_id", originalOrderID).
		Msg("Interaction for redispatching order handled successfully")
}

func (s *DiscordService) handleCancelInteraction(i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID
	orderID := strings.TrimPrefix(customID, "cancel_")

	// 獲取用戶名稱
	userName := "Discord用戶"
	if i.Member != nil && i.Member.User != nil {
		userName = i.Member.User.Username
	} else if i.User != nil {
		userName = i.User.Username
	}

	s.logger.Info().
		Str("order_id", orderID).
		Str("user", userName).
		Msg("Processing Discord cancel interaction")

	// 使用現有的取消邏輯
	s.processCancelCommand(context.Background(), orderID, i.ChannelID, userName)

	s.logger.Info().
		Str("order_id", orderID).
		Str("user", userName).
		Msg("Interaction for cancelling order handled successfully")
}

// processCancelCommand 處理取消指令的實際邏輯
func (s *DiscordService) processCancelCommand(ctx context.Context, orderID, channelID, userName string) {
	s.logger.Info().
		Str("order_id", orderID).
		Str("channel_id", channelID).
		Str("user_name", userName).
		Msg("處理 Discord 取消指令")

	// 嘗試獲取完整訂單ID（支援 ShortID）
	currentOrder, err := s.getOrderByIDOrShortID(ctx, orderID)
	if err != nil {
		s.logger.Error().Err(err).Str("order_id", orderID).Msg("訂單不存在")
		if _, sendErr := s.SendMessage(channelID, "❌ 訂單不存在或已被刪除"); sendErr != nil {
			s.logger.Error().Err(sendErr).Msg("發送訂單不存在錯誤訊息失敗")
		}
		return
	}

	// 使用統一的取消服務（內部會判斷是否為預約單並處理司機狀態）
	updatedOrder, err := s.orderService.CancelOrder(ctx, currentOrder.ID.Hex(), "Discord取消", userName)
	if err != nil {
		s.logger.Error().Err(err).Str("order_id", orderID).Msg("Discord取消訂單失敗")
		if _, sendErr := s.SendMessage(channelID, fmt.Sprintf("❌ %s", err.Error())); sendErr != nil {
			s.logger.Error().Err(sendErr).Msg("發送取消訂單失敗訊息失敗")
		}
		return
	}

	// 4. 回覆成功訊息
	s.logger.Info().
		Str("order_id", orderID).
		Str("short_id", updatedOrder.ShortID).
		Str("previous_status", string(currentOrder.Status)).
		Str("user_name", userName).
		Msg("Discord 取消訂單成功")

	if _, err = s.SendMessage(channelID, fmt.Sprintf("✅ 訂單 %s 已成功取消", updatedOrder.ShortID)); err != nil {
		s.logger.Error().Err(err).Msg("發送取消訂單成功訊息失敗")
	}
}

// getOrderByIDOrShortID 嘗試根據 ID 或 ShortID 獲取訂單
func (s *DiscordService) getOrderByIDOrShortID(ctx context.Context, orderID string) (*model.Order, error) {
	// 先嘗試完整的 ObjectID
	if order, err := s.orderService.GetOrderByID(ctx, orderID); err == nil {
		s.logger.Debug().
			Str("order_id", orderID).
			Msg("通過 ObjectID 找到訂單")
		return order, nil
	}

	// 如果失敗，嘗試通過 ShortID 查找
	if order, err := s.orderService.GetOrderByShortID(ctx, orderID); err == nil {
		s.logger.Debug().
			Str("short_id", orderID).
			Str("object_id", order.ID.Hex()).
			Msg("通過 ShortID 找到訂單")
		return order, nil
	}

	s.logger.Warn().
		Str("order_id", orderID).
		Msg("無法通過 ObjectID 或 ShortID 找到訂單")

	return nil, fmt.Errorf("訂單 %s 不存在", orderID)
}

// formatDriverInfo 格式化司機信息，包括姓名、車牌號碼和車輛顏色
func formatDriverInfo(name, carNo, carColor string) string {
	if carColor != "" {
		return fmt.Sprintf("%s - %s(%s)", name, carNo, carColor)
	}
	return fmt.Sprintf("%s - %s", name, carNo)
}

// formatArrivalDeviation 格式化早到/遲到資訊
func formatArrivalDeviation(deviationSecs *int) string {
	if deviationSecs == nil {
		return ""
	}

	deviation := *deviationSecs
	if deviation == 0 {
		return "🟢 準時抵達"
	}

	absDeviation := deviation
	if absDeviation < 0 {
		absDeviation = -absDeviation
	}

	mins := absDeviation / 60
	secs := absDeviation % 60

	var timeStr string
	if mins > 0 && secs > 0 {
		timeStr = fmt.Sprintf("%d分%d秒", mins, secs)
	} else if mins > 0 {
		timeStr = fmt.Sprintf("%d分鐘", mins)
	} else {
		timeStr = fmt.Sprintf("%d秒", absDeviation)
	}

	if deviation > 0 {
		return fmt.Sprintf("‼️ 遲到%s", timeStr)
	} else {
		return fmt.Sprintf("🟢 提前%s", timeStr)
	}
}

// FormatEventReply 格式化 SSE 事件回覆消息
// 格式: 【車隊名稱#shortid－事件中文名稱】: 原始訂單文字 | 車牌號碼(顏色) | 司機姓名 | 距離km(預估分鐘)
// 對於訂單失敗，只顯示到原始訂單文字
func (s *DiscordService) FormatEventReply(fleet, shortID, eventName, oriText, carPlate, carColor, driverName string, distanceKm float64, estimatedMins int) string {
	// 對於訂單失敗，只顯示到 ori_text
	if eventName == "訂單失敗" {
		return fmt.Sprintf("【%s%s－%s】: %s", fleet, shortID, eventName, oriText)
	}

	var carInfo string
	if carColor != "" {
		carInfo = fmt.Sprintf("%s(%s)", carPlate, carColor)
	} else {
		carInfo = carPlate
	}

	distanceInfo := fmt.Sprintf("%.1fkm(%d分)", distanceKm, estimatedMins)

	return fmt.Sprintf("【%s%s－%s】: %s | %s | %s | %s", fleet, shortID, eventName, oriText, carInfo, driverName, distanceInfo)
}

// FormatEventReplyWithoutDistance 格式化不顯示距離時間的事件回覆消息
// 格式: 【車隊名稱#shortid－事件中文名稱】: 原始訂單文字 | 車牌號碼(顏色) | 司機姓名
func (s *DiscordService) FormatEventReplyWithoutDistance(fleet, shortID, eventName, oriText, carPlate, carColor, driverName string) string {
	var carInfo string
	if carColor != "" {
		carInfo = fmt.Sprintf("%s(%s)", carPlate, carColor)
	} else {
		carInfo = carPlate
	}

	return fmt.Sprintf("【%s%s－%s】: %s | %s | %s", fleet, shortID, eventName, oriText, carInfo, driverName)
}

// FormatScheduledEventReply 格式化預約單事件回覆消息
// 格式: 【車隊名稱#shortid－事件中文名稱】: 預約單 | 原始訂單文字 | 車牌號碼(顏色) | 司機姓名
func (s *DiscordService) FormatScheduledEventReply(fleet, shortID, eventName, oriText, carPlate, carColor, driverName string) string {
	var carInfo string
	if carColor != "" {
		carInfo = fmt.Sprintf("%s(%s)", carPlate, carColor)
	} else {
		carInfo = carPlate
	}

	return fmt.Sprintf("【%s%s－%s】: 預約單 | %s | %s | %s", fleet, shortID, eventName, oriText, carInfo, driverName)
}

// GetEventChineseName 獲取事件的中文名稱
func (s *DiscordService) GetEventChineseName(eventType string) string {
	switch eventType {
	case "driver_accepted_order":
		return "司機接單"
	case "scheduled_accepted":
		return "預約單已被接收"
	case "scheduled_activated":
		return "司機接單"
	case "driver_rejected_order":
		return "司機拒單"
	case "driver_timeout_order":
		return "司機逾時"
	case "driver_arrived":
		return "司機抵達(調度請通知乘客)"
	case "customer_on_board":
		return "客人上車"
	case "order_completed":
		return "訂單完成"
	case "order_failed":
		return "訂單失敗"
	case "order_cancelled":
		return "單號取消成功"
	default:
		return eventType
	}
}

// GetEventColor 根據事件類型獲取Discord embed顏色
func (s *DiscordService) GetEventColor(eventType string) int {
	switch model.EventType(eventType) {
	case model.EventDriverAccepted:
		return int(model.ColorSuccess) // 綠色 - 司機接單
	case model.EventDriverArrived:
		return int(model.ColorProgress) // 橙色 - 司機抵達
	case model.EventCustomerOnBoard:
		return int(model.ColorInfo) // 藍色 - 客人上車
	case model.EventOrderCompleted:
		return int(model.ColorComplete) // 深綠色 - 訂單完成
	case model.EventDriverRejected:
		return int(model.ColorRejected) // 橙紅色 - 司機拒單
	case model.EventDriverTimeout:
		return int(model.ColorWarning) // 琥珀色 - 司機逾時
	case model.EventOrderFailed:
		return int(model.ColorError) // 紅色 - 訂單失敗
	case model.EventOrderCancelled:
		return int(model.ColorCancelled) // 灰色 - 訂單取消
	case model.EventChat:
		return int(model.ColorChat) // 紫色 - 聊天消息
	case model.EventScheduledAccepted:
		return int(model.ColorInfo) // 藍色 - 預約單接受
	case model.EventScheduledActivated:
		return int(model.ColorSuccess) // 綠色 - 預約單激活（司機開始前往）
	default:
		return int(model.ColorDefault) // 默認深灰色
	}
}

// Close closes the Discord session.
func (s *DiscordService) Close() {
	if err := s.session.Close(); err != nil {
		s.logger.Error().Err(err).Msg("關閉Discord連接失敗")
	}
}

// parseOrderIDFromFooter 從Discord embed footer中解析OrderID（簡化版）
func (s *DiscordService) parseOrderIDFromFooter(message *discordgo.Message) (string, error) {
	// 檢查消息是否有embed
	if len(message.Embeds) == 0 {
		return "", fmt.Errorf("消息沒有embed")
	}

	embed := message.Embeds[0]

	// 從footer解析orderID
	if embed.Footer != nil && embed.Footer.Text != "" {
		footerText := embed.Footer.Text

		// 直接orderID格式（新的簡潔格式）
		if len(footerText) == 24 && strings.Contains(footerText, "c") {
			s.logger.Debug().
				Str("footer_text", footerText).
				Str("parsed_order_id", footerText).
				Msg("從footer解析OrderID成功")
			return footerText, nil
		}

		// emoji格式：🔗 orderID（向後兼容）
		if strings.HasPrefix(footerText, "🔗 ") {
			orderID := strings.TrimPrefix(footerText, "🔗 ")
			if orderID != "" {
				s.logger.Debug().
					Str("footer_text", footerText).
					Str("parsed_order_id", orderID).
					Msg("從footer解析OrderID成功（emoji格式）")
				return orderID, nil
			}
		}

		// 舊格式：Order: orderID（向後兼容）
		if strings.HasPrefix(footerText, "Order: ") {
			orderID := strings.TrimPrefix(footerText, "Order: ")
			if orderID != "" {
				s.logger.Debug().
					Str("footer_text", footerText).
					Str("parsed_order_id", orderID).
					Msg("從footer解析OrderID成功（舊格式）")
				return orderID, nil
			}
		}
	}

	return "", fmt.Errorf("無法從embed footer解析OrderID")
}

// handleDiscordReplyMessage 處理Discord回覆消息，將其轉發給對應的司機
func (s *DiscordService) handleDiscordReplyMessage(m *discordgo.MessageCreate) {
	s.logger.Info().
		Str("author", m.Author.Username).
		Str("content", m.Content).
		Str("referenced_message_id", m.ReferencedMessage.ID).
		Msg("處理Discord回覆消息")

	if s.chatService == nil {
		s.logger.Warn().Msg("ChatService未初始化，無法處理Discord回覆")
		return
	}

	// 優先從footer解析OrderID（簡潔方案）
	orderID, err := s.parseOrderIDFromFooter(m.ReferencedMessage)
	if err != nil {
		// 降級使用原有解析邏輯（向後兼容）
		s.logger.Warn().Err(err).
			Str("channel_id", m.ChannelID).
			Str("referenced_message_id", m.ReferencedMessage.ID).
			Msg("無法從footer解析OrderID，嘗試原有邏輯")

		orderID, err = s.findOrderByDiscordMessage(m.ChannelID, m.ReferencedMessage.ID)
		if err != nil {
			s.logger.Error().Err(err).
				Str("channel_id", m.ChannelID).
				Str("referenced_message_id", m.ReferencedMessage.ID).
				Msg("無法找到對應的訂單")
			return
		}
	}

	// 獲取訂單信息以找到司機ID
	order, err := s.orderService.GetOrderByID(context.Background(), orderID)
	if err != nil {
		s.logger.Error().Err(err).
			Str("order_id", orderID).
			Msg("獲取訂單信息失敗")
		return
	}

	if order.Driver.AssignedDriver == "" {
		s.logger.Warn().
			Str("order_id", orderID).
			Msg("訂單沒有分配司機，無法發送聊天消息")
		return
	}

	// 創建或獲取聊天房間
	_, err = s.chatService.CreateOrGetChatRoom(context.Background(), orderID, order.Driver.AssignedDriver)
	if err != nil {
		s.logger.Error().Err(err).
			Str("order_id", orderID).
			Str("driver_id", order.Driver.AssignedDriver).
			Msg("創建聊天房間失敗")
		return
	}

	// 發送消息到聊天系統（使用真實Discord用戶名）
	content := m.Content
	discordUsername := fmt.Sprintf("discord_%s", m.Author.Username) // 使用真實Discord用戶名
	_, err = s.chatService.SendMessage(
		context.Background(),
		orderID,
		discordUsername, // 使用真實的Discord用戶名作為sender
		model.SenderTypeSupport,
		model.MessageTypeText,
		&content,
		nil, // audioURL
		nil, // imageURL
		nil, // audioDuration
		nil, // tempID
	)

	if err != nil {
		s.logger.Error().Err(err).
			Str("order_id", orderID).
			Str("driver_id", order.Driver.AssignedDriver).
			Str("content", content).
			Msg("發送聊天消息失敗")
		return
	}

	s.logger.Info().
		Str("order_id", orderID).
		Str("driver_id", order.Driver.AssignedDriver).
		Str("content", content).
		Str("author", m.Author.Username).
		Msg("Discord回覆消息已成功轉發給司機")

	// 發送FCM推送通知給司機
	go s.sendChatFCMNotification(context.Background(), order.Driver.AssignedDriver, content, order)

	// 向Discord發送確認回應
	err = s.session.MessageReactionAdd(m.ChannelID, m.ID, "✅")
	if err != nil {
		s.logger.Error().Err(err).Msg("添加Discord回應表情失敗")
	}
}

// findOrderByDiscordMessage 根據Discord消息ID查找對應的訂單ID
func (s *DiscordService) findOrderByDiscordMessage(channelID, messageID string) (string, error) {
	// 方法1：直接查詢數據庫（訂單卡片消息）
	order, err := s.orderService.GetOrderByDiscordMessage(context.Background(), channelID, messageID)
	if err == nil {
		s.logger.Info().
			Str("channel_id", channelID).
			Str("message_id", messageID).
			Str("order_id", order.ID.Hex()).
			Msg("通過數據庫直接找到訂單")
		return order.ID.Hex(), nil
	}

	// 方法2：從Discord消息footer解析orderID（統一方案）
	message, err := s.session.ChannelMessage(channelID, messageID)
	if err != nil {
		return "", fmt.Errorf("無法獲取Discord消息: %w", err)
	}

	orderID, err := s.parseOrderIDFromFooter(message)
	if err != nil {
		return "", fmt.Errorf("無法從footer解析orderID: %w", err)
	}

	s.logger.Info().
		Str("channel_id", channelID).
		Str("message_id", messageID).
		Str("order_id", orderID).
		Msg("通過footer找到訂單")

	return orderID, nil
}

// sendChatFCMNotification 發送聊天FCM推送通知給司機
func (s *DiscordService) sendChatFCMNotification(ctx context.Context, driverID, content string, order *model.Order) {
	if s.fcmService == nil {
		s.logger.Warn().Msg("FCM服務未初始化，跳過聊天推送通知")
		return
	}

	// 獲取司機的FCM token
	driver, err := s.orderService.driverService.GetDriverByID(ctx, driverID)
	if err != nil {
		s.logger.Error().Err(err).
			Str("driver_id", driverID).
			Msg("獲取司機信息失敗，無法發送FCM聊天通知")
		return
	}

	if driver.FcmToken == "" {
		s.logger.Debug().
			Str("driver_id", driverID).
			Msg("司機沒有FCM token，跳過聊天推送通知")
		return
	}

	// 準備FCM推送數據
	data := map[string]interface{}{
		"type":      string(model.NotifyTypeChat), // 使用定義的通知類型
		"orderId":   order.ID.Hex(),
		"shortId":   order.ShortID,
		"message":   content,
		"sender":    fmt.Sprintf("discord_%s", "support"), // Discord客服標識
		"timestamp": time.Now().Unix(),
	}

	notification := map[string]interface{}{
		"title": "客服回覆",
		"body":  content,
		"sound": "msg_alert.wav", // 使用聊天音效
	}

	// 發送FCM推送
	err = s.fcmService.Send(ctx, driver.FcmToken, data, notification)
	if err != nil {
		s.logger.Error().Err(err).
			Str("driver_id", driverID).
			Str("fcm_token", driver.FcmToken).
			Str("order_id", order.ID.Hex()).
			Msg("發送聊天FCM推送失敗")
		return
	}

	s.logger.Info().
		Str("driver_id", driverID).
		Str("order_id", order.ID.Hex()).
		Str("content", content).
		Msg("聊天FCM推送發送成功")
}

func (s *DiscordService) messageCreate(sess *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == sess.State.User.ID {
		return
	}

	s.logger.Info().
		Str("message", m.Content).
		Str("author", m.Author.Username).
		Str("channel_id", m.ChannelID).
		Str("message_id", m.ID).
		Msg("Received Discord message")

	// 檢查是否為回覆消息（客服回覆司機聊天）
	if m.ReferencedMessage != nil {
		s.handleDiscordReplyMessage(m)
		return
	}

	// 簡單檢查格式（包含斜線分隔）
	if !strings.Contains(m.Content, "/") {
		return // Not the format we are looking for
	}

	// 檢查是否為重複訊息 - 使用訊息ID作為唯一標識
	// Discord 訊息ID是唯一的，如果我們已經處理過這個ID，就忽略
	messageProcessingKey := fmt.Sprintf("discord_msg_processed:%s", m.ID)

	// 嘗試設置處理標記（5分鐘過期）
	if s.orderService != nil && s.orderService.eventManager != nil {
		// 檢查是否已經處理過這個訊息
		exists, _ := s.orderService.eventManager.GetCache(context.Background(), messageProcessingKey)
		if exists != "" {
			s.logger.Warn().
				Str("message_id", m.ID).
				Str("message_content", m.Content).
				Msg("重複的Discord訊息，已忽略")
			return
		}

		// 標記這個訊息已經開始處理
		if err := s.orderService.eventManager.SetCache(context.Background(), messageProcessingKey, "processing", 5*time.Minute); err != nil {
			s.logger.Warn().Err(err).Msg("設置訊息處理標記失敗")
		}
	}

	// 1. 發送 "創建中" 的卡片消息
	creatingEmbed := &discordgo.MessageEmbed{
		Title:       "⏳ 正在為您建立訂單...",
		Description: m.Content,
		Color:       0x95A5A6, // Grey
		Timestamp:   time.Now().Format(time.RFC3339),
	}
	botMsg, err := s.session.ChannelMessageSendEmbed(m.ChannelID, creatingEmbed)
	if err != nil {
		s.logger.Error().
			Err(err).
			Str("channel_id", m.ChannelID).
			Msg("Error sending initial discord embed")
		return
	}

	// 2. 直接使用 SimpleCreateOrder 來處理用戶輸入
	result, err := s.orderService.SimpleCreateOrder(context.Background(), m.Content, "", model.CreatedByDiscord)
	if err != nil {
		s.logger.Error().
			Err(err).
			Str("order_text", m.Content).
			Msg("Failed to create order from discord")

		// 如果創建失敗，編輯消息為 "失敗" 卡片
		failedEmbed := &discordgo.MessageEmbed{
			Title:       "❌ 訂單建立失敗",
			Description: fmt.Sprintf("**原始指令**:\n%s\n\n**錯誤原因**:\n`%v`", m.Content, err),
			Color:       0xE74C3C, // Red
			Timestamp:   time.Now().Format(time.RFC3339),
		}
		_, err = s.session.ChannelMessageEditEmbed(m.ChannelID, botMsg.ID, failedEmbed)
		if err != nil {
			s.logger.Error().
				Err(err).
				Str("channel_id", m.ChannelID).
				Str("message_id", botMsg.ID).
				Msg("Failed to edit Discord message to failed state")
		}
		return
	}

	createdOrder := result.Order

	// 3. 設置 Discord 相關資訊並更新訂單
	createdOrder.DiscordChannelID = m.ChannelID
	createdOrder.DiscordMessageID = botMsg.ID

	// 更新訂單以保存 Discord 資訊
	updatedOrder, err := s.orderService.UpdateOrder(context.Background(), createdOrder)
	if err != nil {
		s.logger.Error().
			Err(err).
			Str("order_id", createdOrder.ID.Hex()).
			Msg("Failed to update order with discord info")
		// 繼續執行，不阻止訂單更新顯示
		updatedOrder = createdOrder
	}

	// 4. 訂單創建完成，直接更新 Discord 訊息顯示完整訂單資訊
	s.UpdateOrderCard(updatedOrder)
	s.logger.Info().
		Str("order_id", updatedOrder.ID.Hex()).
		Str("short_id", updatedOrder.ShortID).
		Str("status", string(updatedOrder.Status)).
		Str("type", string(updatedOrder.Type)).
		Msg("Discord 訊息已更新顯示完整訂單資訊（包含即時單和預約單）")
}

// createScheduledOrderField 創建預約單欄位（如果訂單是預約單）
func createScheduledOrderField(order *model.Order) *discordgo.MessageEmbedField {
	if order.Type == model.OrderTypeScheduled && order.ScheduledAt != nil {
		// 確保使用台北時區格式化預約時間
		taipeiLocation := time.FixedZone("Asia/Taipei", 8*3600)
		scheduledTime := order.ScheduledAt.In(taipeiLocation).Format("01/02 15:04")
		return &discordgo.MessageEmbedField{
			Name:   "預約單",
			Value:  scheduledTime,
			Inline: true,
		}
	}
	return nil
}

// handleOnlineDriversCommand 處理查詢在線司機指令
func (s *DiscordService) handleOnlineDriversCommand(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	s.logger.Info().Msg("處理查詢在線司機指令")

	// 檢查 DriverService 是否可用
	if s.driverService == nil {
		s.logger.Error().Msg("DriverService 未初始化")
		err := sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ 服務未就緒，請稍後再試",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		if err != nil {
			s.logger.Error().Err(err).Msg("回應服務未就緒失敗")
		}
		return
	}

	// 先回應正在處理中
	err := sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		s.logger.Error().Err(err).Msg("回應處理中失敗")
		return
	}

	// 獲取指令參數
	var fleetFilter string
	if len(i.ApplicationCommandData().Options) > 0 {
		fleetFilter = i.ApplicationCommandData().Options[0].StringValue()
	}

	// 查詢在線司機
	drivers, err := s.driverService.GetOnlineDrivers(context.Background())
	if err != nil {
		s.logger.Error().Err(err).Msg("查詢在線司機失敗")
		_, err = sess.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "❌ 查詢在線司機失敗：" + err.Error(),
			Flags:   discordgo.MessageFlagsEphemeral,
		})
		if err != nil {
			s.logger.Error().Err(err).Msg("發送錯誤回應失敗")
		}
		return
	}

	// 篩選車隊（如果有指定）
	var filteredDrivers []*model.DriverInfo
	if fleetFilter != "" {
		for _, driver := range drivers {
			if string(driver.Fleet) == fleetFilter {
				filteredDrivers = append(filteredDrivers, driver)
			}
		}
	} else {
		filteredDrivers = drivers
	}

	// 格式化回應內容
	response := s.formatOnlineDriversResponse(filteredDrivers, fleetFilter)

	// 發送回應
	_, err = sess.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: response,
		Flags:   discordgo.MessageFlagsEphemeral,
	})
	if err != nil {
		s.logger.Error().Err(err).Msg("發送在線司機列表失敗")
	} else {
		s.logger.Info().
			Int("total_drivers", len(drivers)).
			Int("filtered_drivers", len(filteredDrivers)).
			Str("fleet_filter", fleetFilter).
			Msg("成功發送在線司機列表")
	}
}

// formatOnlineDriversResponse 格式化在線司機回應內容
func (s *DiscordService) formatOnlineDriversResponse(drivers []*model.DriverInfo, fleetFilter string) string {
	if len(drivers) == 0 {
		if fleetFilter != "" {
			return fmt.Sprintf("❌ 目前沒有 %s 車隊的司機在線", fleetFilter)
		}
		return "❌ 目前沒有司機在線"
	}

	var content strings.Builder

	// 標題
	title := fmt.Sprintf("📋 **在線司機** (%d 人)", len(drivers))
	if fleetFilter != "" {
		title = fmt.Sprintf("📋 **%s 車隊在線司機** (%d 人)", fleetFilter, len(drivers))
	}
	content.WriteString(title + "\n\n")

	// 格式化每個司機的信息：車隊 | 車牌號碼(顏色) | 司機姓名
	for _, driver := range drivers {
		carColor := "無色"
		if driver.CarColor != "" {
			carColor = driver.CarColor
		}

		line := fmt.Sprintf("%s | %s(%s) | %s",
			string(driver.Fleet),
			driver.CarPlate,
			carColor,
			driver.Name)
		content.WriteString(line + "\n")
	}

	return content.String()
}

// handleWeiEmptyOrderAndDriverCommand 處理清空 WEI 車隊訂單和司機狀態指令
func (s *DiscordService) handleWeiEmptyOrderAndDriverCommand(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	s.logger.Info().
		Str("command", string(model.SlashCommandWeiEmptyOrderAndDriver)).
		Msg("開始處理清空WEI車隊訂單和司機狀態指令")

	// 獲取用戶名稱
	userName := "Discord用戶"
	if i.Member != nil && i.Member.User != nil {
		userName = i.Member.User.Username
	} else if i.User != nil {
		userName = i.User.Username
	}

	// 先回應用戶，表示正在處理
	err := sess.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "⏳ 正在清空 WEI 車隊的所有訂單並重置司機狀態...",
		},
	})
	if err != nil {
		s.logger.Error().Err(err).Str("user", userName).Msg("回應 WEI 清空指令失敗")
		return
	}

	// 在背景執行清空操作
	go s.processWeiEmptyOrderAndDriver(context.Background(), i, userName)
}

// processWeiEmptyOrderAndDriver 執行清空 WEI 車隊訂單和司機狀態的背景任務
func (s *DiscordService) processWeiEmptyOrderAndDriver(ctx context.Context, i *discordgo.InteractionCreate, userName string) {
	s.logger.Info().
		Str("user", userName).
		Msg("開始清空WEI車隊訂單和司機狀態")

	var successMessages []string
	var errorMessages []string

	// 步驟1: 刪除 WEI 車隊的所有訂單
	if s.orderService != nil {
		deletedCount, err := s.orderService.DeleteAllOrdersByFleet(ctx, model.FleetTypeWEI)
		if err != nil {
			errorMsg := fmt.Sprintf("刪除WEI車隊訂單失敗：%v", err)
			errorMessages = append(errorMessages, errorMsg)
			s.logger.Error().Err(err).Msg(errorMsg)
		} else {
			successMsg := fmt.Sprintf("✅ 成功刪除 %d 個WEI車隊訂單", deletedCount)
			successMessages = append(successMessages, successMsg)
			s.logger.Info().Int("deleted_count", deletedCount).Msg("成功刪除WEI車隊訂單")
		}
	} else {
		errorMessages = append(errorMessages, "❌ OrderService 未初始化，無法刪除訂單")
	}

	// 步驟2: 重置 WEI 車隊司機狀態
	if s.driverService != nil {
		resetCount, err := s.resetWeiDriversStatus(ctx)
		if err != nil {
			errorMsg := fmt.Sprintf("重置WEI車隊司機狀態失敗：%v", err)
			errorMessages = append(errorMessages, errorMsg)
			s.logger.Error().Err(err).Msg(errorMsg)
		} else {
			successMsg := fmt.Sprintf("✅ 成功重置 %d 個WEI車隊司機狀態", resetCount)
			successMessages = append(successMessages, successMsg)
			s.logger.Info().Int("reset_count", resetCount).Msg("成功重置WEI車隊司機狀態")
		}
	} else {
		errorMessages = append(errorMessages, "❌ DriverService 未初始化，無法重置司機狀態")
	}

	// 構建回應訊息
	var responseContent strings.Builder
	responseContent.WriteString("🔧 **WEI車隊清空操作完成**\n\n")

	if len(successMessages) > 0 {
		responseContent.WriteString("**成功操作：**\n")
		for _, msg := range successMessages {
			responseContent.WriteString(msg + "\n")
		}
		responseContent.WriteString("\n")
	}

	if len(errorMessages) > 0 {
		responseContent.WriteString("**錯誤操作：**\n")
		for _, msg := range errorMessages {
			responseContent.WriteString(msg + "\n")
		}
	}

	responseContent.WriteString(fmt.Sprintf("👥 操作人員：%s", userName))

	// 發送最終結果
	_, err := s.session.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: responseContent.String(),
	})
	if err != nil {
		s.logger.Error().Err(err).Msg("回應WEI車隊清空操作結果失敗")
	}
}

// resetWeiDriversStatus 重置所有 WEI 車隊司機的狀態
func (s *DiscordService) resetWeiDriversStatus(ctx context.Context) (int, error) {
	// 獲取所有 WEI 車隊的司機
	drivers, err := s.driverService.GetDriversByFleet(ctx, model.FleetTypeWEI)
	if err != nil {
		return 0, fmt.Errorf("獲取WEI車隊司機失敗: %w", err)
	}

	resetCount := 0
	for _, driver := range drivers {
		// 使用 ResetDriverWithScheduleClear 重置司機狀態並清除所有訂單相關字段
		_, err := s.driverService.ResetDriverWithScheduleClear(ctx, driver.ID.Hex(), "Discord-WEI-清空指令")
		if err != nil {
			s.logger.Error().Err(err).
				Str("driver_id", driver.ID.Hex()).
				Str("driver_name", driver.Name).
				Msg("重置WEI司機狀態失敗")
			continue
		}
		resetCount++
	}

	return resetCount, nil
}

// handleWeiCreateExampleOrderCommand 處理建立 WEI 測試訂單指令
func (s *DiscordService) handleWeiCreateExampleOrderCommand(sess *discordgo.Session, i *discordgo.InteractionCreate) {
	s.logger.Info().
		Str("command", string(model.SlashCommandWeiCreateExampleOrder)).
		Msg("開始處理建立WEI測試訂單指令")

	// 獲取用戶名稱
	userName := "Discord用戶"
	if i.Member != nil && i.Member.User != nil {
		userName = i.Member.User.Username
	} else if i.User != nil {
		userName = i.User.Username
	}

	// 獲取訂單類型參數（預設為即時單）
	orderType := "instant" // 預設值
	options := i.ApplicationCommandData().Options
	if len(options) > 0 {
		for _, option := range options {
			if option.Name == "type" {
				orderType = option.StringValue()
			}
		}
	}

	// 直接在背景執行建立訂單操作，讓 processWeiCreateExampleOrder 處理 interaction response
	go s.processWeiCreateExampleOrder(context.Background(), i, userName, orderType)
}

// processWeiCreateExampleOrder 執行建立 WEI 測試訂單的背景任務
func (s *DiscordService) processWeiCreateExampleOrder(ctx context.Context, i *discordgo.InteractionCreate, userName string, orderType string) {
	s.logger.Info().
		Str("user", userName).
		Str("order_type", orderType).
		Msg("開始建立WEI測試訂單")

	if s.orderService == nil {
		_, err := s.session.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "❌ OrderService 未初始化，無法建立測試訂單",
		})
		if err != nil {
			s.logger.Error().Err(err).Msg("回應 OrderService 未初始化錯誤失敗")
		}
		return
	}

	// 生成隨機數字 (100-120)
	randomNum := rand.Intn(21) + 100 // 生成 0-20 的隨機數，然後加上 100

	var content string
	if orderType == "scheduled" {
		// 預約單：當前時間 +1小時05分
		scheduledTime := time.Now().Add(1*time.Hour + 5*time.Minute)
		timeStr := scheduledTime.Format("15:04")
		content = fmt.Sprintf("W0/638台灣雲林縣麥寮鄉中山路%d號 %s", randomNum, timeStr)
	} else {
		// 即時單
		content = fmt.Sprintf("W0/638台灣雲林縣麥寮鄉中山路%d號 測試即時單", randomNum)
	}

	s.logger.Info().
		Str("content", content).
		Str("order_type", orderType).
		Int("random_num", randomNum).
		Msg("生成的測試訂單內容")

	// 1. 使用 interaction response 發送 "創建中" 的卡片消息
	creatingEmbed := &discordgo.MessageEmbed{
		Title:       "⏳ 正在為您建立訂單...",
		Description: content,
		Color:       0x95A5A6, // Grey
		Timestamp:   time.Now().Format(time.RFC3339),
	}

	// 使用 interaction response 發送初始 embed
	err := s.session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{creatingEmbed},
		},
	})
	if err != nil {
		s.logger.Error().Err(err).Str("channel_id", i.ChannelID).Msg("發送初始 interaction response embed 失敗")
		return
	}

	// 取得 interaction response message，以便後續更新
	botMsg, err := s.session.InteractionResponse(i.Interaction)
	if err != nil {
		s.logger.Error().Err(err).Msg("取得 interaction response message 失敗")
		return
	}

	// 2. 建立訂單
	result, err := s.orderService.SimpleCreateOrder(ctx, content, "", model.CreatedByDiscord)
	if err != nil {
		s.logger.Error().Err(err).
			Str("content", content).
			Str("order_type", orderType).
			Msg("建立WEI測試訂單失敗")

		// 編輯 interaction response embed 為失敗狀態
		failedEmbed := &discordgo.MessageEmbed{
			Title:       "❌ 訂單建立失敗",
			Description: fmt.Sprintf("**原始指令**:\n%s\n\n**錯誤原因**:\n`%v`", content, err),
			Color:       0xE74C3C, // Red
			Timestamp:   time.Now().Format(time.RFC3339),
		}
		_, editErr := s.session.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Embeds: &[]*discordgo.MessageEmbed{failedEmbed},
		})
		if editErr != nil {
			s.logger.Error().Err(editErr).Msg("編輯失敗 interaction response embed 失敗")
		}
		return
	}

	createdOrder := result.Order

	// 3. 設置 Discord 相關資訊並更新訂單（模擬文字輸入流程）
	createdOrder.DiscordChannelID = i.ChannelID
	createdOrder.DiscordMessageID = botMsg.ID

	// 更新訂單以保存 Discord 資訊
	updatedOrder, err := s.orderService.UpdateOrder(ctx, createdOrder)
	if err != nil {
		s.logger.Error().Err(err).
			Str("order_id", createdOrder.ID.Hex()).
			Msg("更新訂單 Discord 資訊失敗")
	} else {
		s.logger.Info().
			Str("order_id", updatedOrder.ID.Hex()).
			Str("discord_channel_id", updatedOrder.DiscordChannelID).
			Str("discord_message_id", updatedOrder.DiscordMessageID).
			Msg("訂單 Discord 資訊更新成功")
	}

	// 4. 手動生成訂單字卡並更新 interaction response（不能使用 UpdateOrderCard 因為那是針對一般消息）
	s.updateInteractionResponseOrderCard(i, updatedOrder)

	s.logger.Info().
		Str("order_id", result.Order.ID.Hex()).
		Str("short_id", result.Order.ShortID).
		Str("content", content).
		Str("order_type", orderType).
		Str("user", userName).
		Msg("WEI測試訂單建立成功，字卡已生成")

	// 5. 發送成功確認訊息到 slash command 回應
	typeDisplay := "即時單"
	if orderType == "scheduled" {
		typeDisplay = "預約單"
	}

	successMessage := fmt.Sprintf("✅ **WEI車隊%s建立成功**\n"+
		"🆔 訂單編號：%s\n"+
		"📍 地址：台灣雲林縣麥寮鄉中山路%d號\n"+
		"📝 內容：%s\n"+
		"👥 建立者：%s\n"+
		"🎯 訂單字卡已在上方顯示",
		typeDisplay,
		result.Order.ShortID,
		randomNum,
		content,
		userName)

	// 發送成功結果
	_, err = s.session.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: successMessage,
	})
	if err != nil {
		s.logger.Error().Err(err).Msg("回應建立WEI測試訂單成功訊息失敗")
	}
}

// updateInteractionResponseOrderCard 更新 interaction response 為訂單字卡
func (s *DiscordService) updateInteractionResponseOrderCard(i *discordgo.InteractionCreate, order *model.Order) {
	shortID := order.ShortID
	embed := &discordgo.MessageEmbed{
		Type: discordgo.EmbedTypeRich,
		Footer: &discordgo.MessageEmbedFooter{
			Text: shortID,
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	var components []discordgo.MessageComponent

	switch order.Status {
	case model.OrderStatusWaiting:
		// 特別處理等待接單狀態
		if order.Type == model.OrderTypeScheduled {
			embed.Title = fmt.Sprintf("⏳ 等待接單－預約單 (%s)", shortID)
			embed.Color = 0xF1C40F // Yellow for scheduled orders
		} else if order.Type == model.OrderTypeInstant {
			// 判斷是否為預約單轉換而來的即時單
			if order.ConvertedFrom == "scheduled" {
				embed.Title = fmt.Sprintf("🔄 轉換即時單－等待接單 (%s)", shortID)
				embed.Color = 0xFF6B6B // Red for converted instant orders
			} else {
				embed.Title = fmt.Sprintf("⏳ 等待接單－即時單 (%s)", shortID)
				embed.Color = 0x3498DB // Blue for regular instant orders
			}
		}

		defaultFields := []*discordgo.MessageEmbedField{
			{Name: "客戶群組", Value: order.CustomerGroup, Inline: true},
			{Name: "狀態", Value: string(order.Status), Inline: true},
			{Name: "上車地點", Value: order.OriText, Inline: false},
		}
		// 添加預約單欄位（如果是預約單）
		if scheduledField := createScheduledOrderField(order); scheduledField != nil {
			defaultFields = append(defaultFields, scheduledField)
		}
		// 添加 Google 地址（如果存在）
		if order.Customer.PickupAddress != "" {
			defaultFields = append(defaultFields, &discordgo.MessageEmbedField{
				Name: "Google地址", Value: order.Customer.PickupAddress, Inline: false,
			})
		}
		defaultFields = append(defaultFields, &discordgo.MessageEmbedField{
			Name: "備註", Value: order.Customer.Remarks, Inline: false,
		})
		embed.Fields = defaultFields

		// 為可取消的狀態添加取消按鈕
		components = []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{
						Label:    "取消訂單",
						Style:    discordgo.SecondaryButton,
						CustomID: "cancel_" + order.ID.Hex(),
						Emoji:    &discordgo.ComponentEmoji{Name: "❌"},
					},
				},
			},
		}

	default:
		// 其他狀態使用預設處理
		embed.Title = fmt.Sprintf("⏳ %s (%s)", string(order.Status), shortID)
		embed.Color = 0x95A5A6 // Grey
		defaultFields := []*discordgo.MessageEmbedField{
			{Name: "客戶群組", Value: order.CustomerGroup, Inline: true},
			{Name: "狀態", Value: string(order.Status), Inline: true},
			{Name: "上車地點", Value: order.OriText, Inline: false},
		}
		// 添加預約單欄位（如果是預約單）
		if scheduledField := createScheduledOrderField(order); scheduledField != nil {
			defaultFields = append(defaultFields, scheduledField)
		}
		// 添加 Google 地址（如果存在）
		if order.Customer.PickupAddress != "" {
			defaultFields = append(defaultFields, &discordgo.MessageEmbedField{
				Name: "Google地址", Value: order.Customer.PickupAddress, Inline: false,
			})
		}
		defaultFields = append(defaultFields, &discordgo.MessageEmbedField{
			Name: "備註", Value: order.Customer.Remarks, Inline: false,
		})
		embed.Fields = defaultFields
	}

	// 更新 interaction response
	_, err := s.session.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds:     &[]*discordgo.MessageEmbed{embed},
		Components: &components,
	})
	if err != nil {
		s.logger.Error().
			Err(err).
			Str("order_id", order.ID.Hex()).
			Msg("更新 interaction response 為訂單字卡失敗")
	} else {
		s.logger.Info().
			Str("order_id", order.ID.Hex()).
			Str("short_id", shortID).
			Msg("成功更新 interaction response 為訂單字卡")
	}
}
