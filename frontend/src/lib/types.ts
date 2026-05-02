export type UserRole = 'coach' | 'client'

export interface AuthUser {
  id: string
  role: UserRole
  email: string
  full_name: string
}

export type SessionStatus =
  | 'proposed'
  | 'confirmed'
  | 'cancelled'
  | 'completed'
  | 'pending_cancellation'

export interface Session {
  id: string
  coach_id: string
  client_id: string
  schedule_run_id?: string
  starts_at: string
  ends_at: string
  status: SessionStatus
  notes?: string
  cancellation_reason?: string
  cancellation_requested_at?: string
  client_name?: string
  coach_name?: string
  created_at: string
  updated_at: string
}

export type ScheduleRunStatus =
  | 'pending_confirmation'
  | 'confirmed'
  | 'rejected'
  | 'expired'

export interface ScheduleRun {
  id: string
  coach_id: string
  week_start: string
  status: ScheduleRunStatus
  sessions: Session[]
  expires_at: string
  created_at: string
  updated_at: string
}

export interface TimeSlot {
  day_of_week: number
  start_time: string
  end_time: string
}

export interface CoachProfile {
  user: {
    id: string
    email: string
    role: string
    full_name: string
    phone?: string
    timezone: string
  }
  coach: {
    id: string
    user_id: string
    business_name?: string
    max_sessions_per_day: number
  }
}

export interface UpdateCoachProfileRequest {
  full_name: string
  business_name?: string
  phone?: string
  timezone: string
  max_sessions_per_day: number
}

export interface ClientProfile {
  user: {
    id: string
    email: string
    role: string
    full_name: string
    phone?: string
    timezone: string
  }
  client: {
    id: string
    user_id: string
    coach_id: string
    tenure_started_at: string
    sessions_per_month: number
    priority_score: number
  }
}

export interface CoachClientSummary {
  user: {
    id: string
    email: string
    role: string
    full_name: string
    phone?: string
    timezone: string
    is_verified: boolean
  }
  client: {
    id: string
    user_id: string
    coach_id: string
    tenure_started_at: string
    sessions_per_month: number
    priority_score: number
  }
  confirmed_session_count: number
}

export interface CreateCoachClientInput {
  email: string
  full_name: string
  phone?: string
  timezone?: string
  sessions_per_month: number
}

export interface CalendarURLResponse {
  url: string
  warning: string
}

export interface SetupIntentResponse {
  client_secret: string
  setup_intent_id: string
}

export interface SessionCredit {
  id: string
  client_id: string
  source_session_id: string
  reason: string
  expires_at: string
  created_at: string
}

export interface CancelSessionResponse {
  session: Session
  credit?: SessionCredit
  within_24h_window: boolean
}

export type TemplateStatus = 'missing' | 'pending' | 'approved' | 'rejected'

export interface AgentSettings {
  coach_id: string
  enabled: boolean
  template_sid?: string
  template_status: TemplateStatus
  prompt_day: string
  prompt_time: string
  timezone: string
  require_coach_confirmation: boolean
  created_at: string
  updated_at: string
}

export interface AgentClient {
  client_id: string
  full_name: string
  email: string
  phone?: string
  ai_booking_enabled: boolean
}

export interface CheckTemplateResponse {
  template_status: TemplateStatus
  rejection_reason?: string
}

export interface AgentOverview {
  campaign_status: string
  week_start?: string
  texted_count: number
  replied_count: number
  waiting_count: number
  parsed_count?: number
}

export interface SubscriptionPlan {
  id: string
  coach_id: string
  name: string
  description?: string
  sessions_included: number
  amount_pence: number
  active: boolean
  created_at: string
  updated_at: string
}

export interface ClientSubscriptionDetail {
  id: string
  client_id: string
  plan_id: string
  plan_name: string
  sessions_included: number
  status: string
  current_period_start?: string
  current_period_end?: string
  sessions_balance: number
  created_at: string
  updated_at: string
}

export interface ClientSubscriptionView {
  plan_name: string
  sessions_balance: number
  current_period_end?: string
}

export interface PlanChange {
  id: string
  subscription_id: string
  from_plan_id: string
  to_plan_id: string
  requested_by: string
  status: string
  created_at: string
  updated_at: string
}
