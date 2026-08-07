import { api } from './client'
import type { NotificationsResponse } from './types'

export async function fetchUnreadCount(): Promise<number> {
  const data = await api.get<{ count: number }>('/notifications/unread-count')
  return data.count
}

export async function fetchNotifications(page = 1, pageSize = 20): Promise<NotificationsResponse> {
  const data = await api.get<NotificationsResponse>(`/notifications?page=${page}&page_size=${pageSize}`)
  return {
    notifications: Array.isArray(data.notifications) ? data.notifications : [],
    total: data.total ?? 0,
  }
}

export async function markNotificationRead(id: number): Promise<void> {
  await api.patch<void>(`/notifications/${id}/read`, {})
}

export async function deleteNotification(id: number): Promise<void> {
  await api.delete<void>(`/notifications/${id}`)
}

export async function clearAllNotifications(): Promise<void> {
  await api.delete<void>('/notifications')
}
