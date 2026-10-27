-- ============================================================================
-- CRM Sales Intelligence — Rich Demo & Benchmark Data
--
-- Provides 24 realistic leads across all lifecycle stages, sources, and deal
-- values with complete stage histories, activity timelines, team ownership,
-- and tasks. Designed to power accurate Lead Scoring, Sales Funnel Analytics,
-- and Lead Source ROI intelligence.
-- ============================================================================

BEGIN;

-- 1. Ensure team members exist
INSERT INTO team_members (name, email, email_reminders, default_reminder_minutes)
VALUES
    ('Rohan Sharma',  'rohan.sharma@pipelinecrm.io',  true, 15),
    ('Ananya Sen',    'ananya.sen@pipelinecrm.io',    true, 15),
    ('Kavita Rao',    'kavita.rao@pipelinecrm.io',    true, 30),
    ('Vikram Patel',  'vikram.patel@pipelinecrm.io',  true, 15)
ON CONFLICT (lower(email)) DO NOTHING;

-- 2. Clean existing leads & cascades
TRUNCATE leads RESTART IDENTITY CASCADE;

-- 3. Insert realistic multi-stage leads with varied sources and deal values
INSERT INTO leads (name, company, email, phone, source, stage, notes, deal_value, owner_id, close_reason, close_note, created_at, updated_at)
SELECT
    v.name, v.company, v.email, v.phone, v.source::lead_source, v.stage::lead_stage,
    v.notes, v.deal_value, tm.id, v.close_reason, v.close_note, v.created_at, v.updated_at
FROM (VALUES
    -- WON LEADS (High value, full journey)
    ('Ananya Sharma', 'Vertex Analytics', 'ananya@vertexanalytics.io', '+91 98200 11223', 'website', 'won', 'Signed annual contract for the Enterprise Growth tier.', 1250000, 'rohan.sharma@pipelinecrm.io', 'product_fit', 'Superior API and analytics capabilities compared to legacy CRM.', now() - interval '55 days', now() - interval '5 days'),
    ('Rohan Mehta', 'Blue Harbour Logistics', 'rohan.mehta@blueharbour.co', '+91 99870 45611', 'referral', 'won', 'Referred by Vertex. Fast closing cycle, executive sponsor signed.', 850000, 'ananya.sen@pipelinecrm.io', 'referral', 'High trust from mutual partner recommendation.', now() - interval '48 days', now() - interval '8 days'),
    ('Aditya Chopra', 'FinScale Technologies', 'aditya@finscale.in', '+91 98110 99887', 'linkedin', 'won', 'Closed 3-year multi-team rollout. Implementation starts next week.', 1600000, 'rohan.sharma@pipelinecrm.io', 'service', 'Rapid custom demo tailored to their core payment flow.', now() - interval '42 days', now() - interval '10 days'),
    ('Elena Rostova', 'Apex Global Logistics', 'elena@apexlogistics.com', '+44 20 7946 0991', 'referral', 'won', 'Global shipment tracking integration required. Won against Salesforce.', 1450000, 'kavita.rao@pipelinecrm.io', 'product_fit', 'Custom webhook automation matched their legacy ERP.', now() - interval '38 days', now() - interval '12 days'),
    ('Karan Singhania', 'OmniRetail Hub', 'karan@omniretail.in', '+91 99201 33445', 'website', 'won', 'Annual contract. Point of sale lead sync confirmed.', 720000, 'ananya.sen@pipelinecrm.io', 'price', 'Competitive annual pricing tier beat existing incumbent.', now() - interval '30 days', now() - interval '4 days'),
    ('Siddharth Joshi', 'CloudNest Systems', 'sid@cloudnest.io', '+91 98332 11990', 'cold_call', 'won', 'Targeted enterprise SDR outreach. 50 seat license closed.', 650000, 'vikram.patel@pipelinecrm.io', 'relationship', 'Proof of concept delivered in 48 hours with executive touch.', now() - interval '34 days', now() - interval '6 days'),

    -- LOST LEADS (With authentic loss reasons)
    ('Laila Haddad', 'Cedar & Co', 'laila@cedarandco.com', '+971 4 555 0198', 'other', 'lost', 'Decided to build an internal spreadsheet and tool set.', 400000, 'kavita.rao@pipelinecrm.io', 'no_budget', 'Q3 marketing and tooling budget frozen by board.', now() - interval '45 days', now() - interval '15 days'),
    ('Vikram Desai', 'Orbit Retail Group', 'vikram@orbitretail.com', '+91 98111 20394', 'cold_call', 'lost', 'Selected enterprise suite that bundled ERP accounting.', 900000, 'vikram.patel@pipelinecrm.io', 'competitor', 'Existing vendor offered 60% bundle discount with ERP.', now() - interval '40 days', now() - interval '18 days'),
    ('Farhan Akhtar', 'Zenith Pharma', 'farhan@zenithpharma.com', '+91 98450 77123', 'website', 'lost', 'Needed FDA 21 CFR Part 11 electronic signature compliance.', 1100000, 'rohan.sharma@pipelinecrm.io', 'product_fit', 'Missing certified FDA compliance export module.', now() - interval '36 days', now() - interval '20 days'),
    ('Nathalie Dubois', 'Lumiere Media', 'nathalie@lumieremedia.fr', '+33 1 42 68 55 00', 'linkedin', 'lost', 'Postponed CRM migration until next fiscal year.', 550000, 'ananya.sen@pipelinecrm.io', 'timing', 'Company restructuring deferred all software onboarding.', now() - interval '29 days', now() - interval '14 days'),

    -- NEGOTIATION (Hot active deals)
    ('Priya Nair', 'Kestrel Foods', 'priya.nair@kestrelfoods.com', '+91 90045 77812', 'linkedin', 'negotiation', 'Finalizing SLA terms and 2-year pricing structure with legal.', 950000, 'ananya.sen@pipelinecrm.io', NULL, NULL, now() - interval '28 days', now() - interval '1 days'),
    ('Marcus Vance', 'Vance Robotics Corp', 'marcus@vancerobotics.com', '+1 415 555 0892', 'referral', 'negotiation', 'Contract drafted. C-level approval expected by Friday.', 1200000, 'rohan.sharma@pipelinecrm.io', NULL, NULL, now() - interval '25 days', now() - interval '2 days'),
    ('Tara Deshmukh', 'Acuity BioLabs', 'tara@acuitybiolabs.com', '+91 98701 44321', 'website', 'negotiation', 'Master Services Agreement under review by procurement.', 800000, 'kavita.rao@pipelinecrm.io', NULL, NULL, now() - interval '22 days', now() - interval '3 days'),

    -- PROPOSAL SENT (Warm progressing pipeline)
    ('Daniel Okoro', 'Northwind Studios', 'daniel@northwindstudios.com', '+44 20 7946 0102', 'website', 'proposal_sent', 'Proposal v2 sent after scope refinement. Following up on review.', 600000, 'ananya.sen@pipelinecrm.io', NULL, NULL, now() - interval '24 days', now() - interval '3 days'),
    ('Sunita Agarwal', 'Evergreen Solar', 'sunita@evergreensolar.in', '+91 98220 88123', 'cold_call', 'proposal_sent', 'Sent tiered proposal for 35 field sales reps.', 520000, 'vikram.patel@pipelinecrm.io', NULL, NULL, now() - interval '20 days', now() - interval '4 days'),
    ('David Chen', 'Pacific Wave Import', 'david.chen@pacificwave.hk', '+852 2345 6789', 'referral', 'proposal_sent', 'Custom multi-currency quote delivered. Positive feedback.', 880000, 'rohan.sharma@pipelinecrm.io', NULL, NULL, now() - interval '18 days', now() - interval '2 days'),
    ('Aarti Kapoor', 'VogueSphere Lifestyle', 'aarti@voguesphere.com', '+91 99300 22119', 'linkedin', 'proposal_sent', 'Demo completed. Security and architecture document shared.', 450000, 'kavita.rao@pipelinecrm.io', NULL, NULL, now() - interval '16 days', now() - interval '5 days'),

    -- QUALIFIED (Confirmed budget & scope)
    ('Meera Iyer', 'Sunloop Energy', 'meera.iyer@sunloop.energy', '+91 88765 30021', 'cold_call', 'qualified', 'Budget confirmed for next quarter. Technical champion: Meera.', 480000, 'vikram.patel@pipelinecrm.io', NULL, NULL, now() - interval '21 days', now() - interval '4 days'),
    ('Tom Whitfield', 'Ledgerline Fintech', 'tom.w@ledgerline.com', '+1 415 555 0132', 'referral', 'qualified', 'Needs SOC2 and ISO27001 compliance audit trail verified.', 750000, 'rohan.sharma@pipelinecrm.io', NULL, NULL, now() - interval '19 days', now() - interval '3 days'),
    ('Neha Malviya', 'HealthBridge AI', 'neha@healthbridgeai.com', '+91 97110 55432', 'website', 'qualified', 'Evaluating replacement for HubSpot. Strong product alignment.', 680000, 'ananya.sen@pipelinecrm.io', NULL, NULL, now() - interval '15 days', now() - interval '2 days'),
    ('Rajesh Somani', 'Bharat Logistics Hub', 'rajesh@bharatlogistics.in', '+91 98210 99001', 'other', 'qualified', 'Met at Logistics Summit. Clear buying intent for 25 branches.', 580000, 'kavita.rao@pipelinecrm.io', NULL, NULL, now() - interval '14 days', now() - interval '4 days'),

    -- CONTACTED (Discovery in progress)
    ('Sneha Kulkarni', 'Arcadia Interiors', 'sneha@arcadiainteriors.in', '+91 97655 12093', 'linkedin', 'contacted', 'Interested in lead distribution rules. Discovery call held.', 320000, 'ananya.sen@pipelinecrm.io', NULL, NULL, now() - interval '12 days', now() - interval '3 days'),
    ('Arjun Bhatia', 'Peakforge Tooling', 'arjun.bhatia@peakforge.dev', '+91 93210 88764', 'website', 'contacted', 'Downloaded pricing and product whitepaper. Follow-up demo scheduled.', 410000, 'rohan.sharma@pipelinecrm.io', NULL, NULL, now() - interval '10 days', now() - interval '2 days'),
    ('Zayn Al-Mansoor', 'Gulf Stream Trading', 'zayn@gulfstream.ae', '+971 4 330 9182', 'cold_call', 'contacted', 'Introductory email opened multiple times. Follow-up call pending.', 350000, 'vikram.patel@pipelinecrm.io', NULL, NULL, now() - interval '9 days', now() - interval '1 days'),

    -- NEW (Fresh inbounds)
    ('Grace Lin', 'Halcyon Health', 'grace.lin@halcyonhealth.io', '+65 6555 0177', 'referral', 'new', 'Inbound referral from partner agency. Highly qualified lead.', 500000, 'rohan.sharma@pipelinecrm.io', NULL, NULL, now() - interval '5 days', now() - interval '5 days'),
    ('Imran Qureshi', 'Trailhead Ventures', 'imran@trailheadvc.com', '+91 90909 44556', 'linkedin', 'new', 'Commented on launch post regarding automated pipeline tracking.', NULL, 'ananya.sen@pipelinecrm.io', NULL, NULL, now() - interval '3 days', now() - interval '3 days'),
    ('Nikita Rao', 'Fablane Apparel', 'nikita.rao@fablane.shop', '+91 98190 33441', 'website', 'new', 'Filled out contact form requesting enterprise quote for 15 users.', 380000, 'kavita.rao@pipelinecrm.io', NULL, NULL, now() - interval '2 days', now() - interval '2 days'),
    ('Samuel Adeyemi', 'Quayside Media', 'samuel@quaysidemedia.tv', '+44 161 555 0143', 'other', 'new', 'Inbound inquiry regarding custom sales reporting dashboards.', NULL, 'vikram.patel@pipelinecrm.io', NULL, NULL, now() - interval '18 hours', now() - interval '18 hours')
) AS v(name, company, email, phone, source, stage, notes, deal_value, owner_email, close_reason, close_note, created_at, updated_at)
LEFT JOIN team_members tm ON lower(tm.email) = v.owner_email;

-- 4. Complete stage progression history (lead_stage_history)
-- Reflects realistic cycle times through each stage for funnel and velocity metrics.
INSERT INTO lead_stage_history (lead_id, from_stage, to_stage, changed_at)
SELECT l.id, h.from_stage::lead_stage, h.to_stage::lead_stage, h.changed_at
FROM leads l
JOIN (VALUES
    -- Vertex Analytics (Won)
    ('ananya@vertexanalytics.io', NULL, 'new', now() - interval '55 days'),
    ('ananya@vertexanalytics.io', 'new', 'contacted', now() - interval '52 days'),
    ('ananya@vertexanalytics.io', 'contacted', 'qualified', now() - interval '45 days'),
    ('ananya@vertexanalytics.io', 'qualified', 'proposal_sent', now() - interval '35 days'),
    ('ananya@vertexanalytics.io', 'proposal_sent', 'negotiation', now() - interval '20 days'),
    ('ananya@vertexanalytics.io', 'negotiation', 'won', now() - interval '5 days'),

    -- Blue Harbour (Won)
    ('rohan.mehta@blueharbour.co', NULL, 'new', now() - interval '48 days'),
    ('rohan.mehta@blueharbour.co', 'new', 'contacted', now() - interval '46 days'),
    ('rohan.mehta@blueharbour.co', 'contacted', 'qualified', now() - interval '40 days'),
    ('rohan.mehta@blueharbour.co', 'qualified', 'proposal_sent', now() - interval '32 days'),
    ('rohan.mehta@blueharbour.co', 'proposal_sent', 'negotiation', now() - interval '19 days'),
    ('rohan.mehta@blueharbour.co', 'negotiation', 'won', now() - interval '8 days'),

    -- FinScale (Won)
    ('aditya@finscale.in', NULL, 'new', now() - interval '42 days'),
    ('aditya@finscale.in', 'new', 'contacted', now() - interval '39 days'),
    ('aditya@finscale.in', 'contacted', 'qualified', now() - interval '33 days'),
    ('aditya@finscale.in', 'qualified', 'proposal_sent', now() - interval '25 days'),
    ('aditya@finscale.in', 'proposal_sent', 'negotiation', now() - interval '17 days'),
    ('aditya@finscale.in', 'negotiation', 'won', now() - interval '10 days'),

    -- Apex Logistics (Won)
    ('elena@apexlogistics.com', NULL, 'new', now() - interval '38 days'),
    ('elena@apexlogistics.com', 'new', 'contacted', now() - interval '35 days'),
    ('elena@apexlogistics.com', 'contacted', 'qualified', now() - interval '30 days'),
    ('elena@apexlogistics.com', 'qualified', 'proposal_sent', now() - interval '24 days'),
    ('elena@apexlogistics.com', 'proposal_sent', 'negotiation', now() - interval '18 days'),
    ('elena@apexlogistics.com', 'negotiation', 'won', now() - interval '12 days'),

    -- OmniRetail (Won)
    ('karan@omniretail.in', NULL, 'new', now() - interval '30 days'),
    ('karan@omniretail.in', 'new', 'contacted', now() - interval '28 days'),
    ('karan@omniretail.in', 'contacted', 'qualified', now() - interval '22 days'),
    ('karan@omniretail.in', 'qualified', 'proposal_sent', now() - interval '16 days'),
    ('karan@omniretail.in', 'proposal_sent', 'negotiation', now() - interval '10 days'),
    ('karan@omniretail.in', 'negotiation', 'won', now() - interval '4 days'),

    -- CloudNest (Won)
    ('sid@cloudnest.io', NULL, 'new', now() - interval '34 days'),
    ('sid@cloudnest.io', 'new', 'contacted', now() - interval '31 days'),
    ('sid@cloudnest.io', 'contacted', 'qualified', now() - interval '25 days'),
    ('sid@cloudnest.io', 'qualified', 'proposal_sent', now() - interval '18 days'),
    ('sid@cloudnest.io', 'proposal_sent', 'negotiation', now() - interval '12 days'),
    ('sid@cloudnest.io', 'negotiation', 'won', now() - interval '6 days'),

    -- Lost deals journeys
    ('laila@cedarandco.com', NULL, 'new', now() - interval '45 days'),
    ('laila@cedarandco.com', 'new', 'contacted', now() - interval '42 days'),
    ('laila@cedarandco.com', 'contacted', 'qualified', now() - interval '35 days'),
    ('laila@cedarandco.com', 'qualified', 'proposal_sent', now() - interval '28 days'),
    ('laila@cedarandco.com', 'proposal_sent', 'lost', now() - interval '15 days'),

    ('vikram@orbitretail.com', NULL, 'new', now() - interval '40 days'),
    ('vikram@orbitretail.com', 'new', 'contacted', now() - interval '36 days'),
    ('vikram@orbitretail.com', 'contacted', 'lost', now() - interval '18 days'),

    ('farhan@zenithpharma.com', NULL, 'new', now() - interval '36 days'),
    ('farhan@zenithpharma.com', 'new', 'contacted', now() - interval '33 days'),
    ('farhan@zenithpharma.com', 'contacted', 'qualified', now() - interval '27 days'),
    ('farhan@zenithpharma.com', 'qualified', 'proposal_sent', now() - interval '23 days'),
    ('farhan@zenithpharma.com', 'proposal_sent', 'lost', now() - interval '20 days'),

    ('nathalie@lumieremedia.fr', NULL, 'new', now() - interval '29 days'),
    ('nathalie@lumieremedia.fr', 'new', 'contacted', now() - interval '26 days'),
    ('nathalie@lumieremedia.fr', 'contacted', 'qualified', now() - interval '20 days'),
    ('nathalie@lumieremedia.fr', 'qualified', 'lost', now() - interval '14 days'),

    -- Active Deals Journeys
    ('priya.nair@kestrelfoods.com', NULL, 'new', now() - interval '28 days'),
    ('priya.nair@kestrelfoods.com', 'new', 'contacted', now() - interval '24 days'),
    ('priya.nair@kestrelfoods.com', 'contacted', 'qualified', now() - interval '18 days'),
    ('priya.nair@kestrelfoods.com', 'qualified', 'proposal_sent', now() - interval '12 days'),
    ('priya.nair@kestrelfoods.com', 'proposal_sent', 'negotiation', now() - interval '3 days'),

    ('marcus@vancerobotics.com', NULL, 'new', now() - interval '25 days'),
    ('marcus@vancerobotics.com', 'new', 'contacted', now() - interval '21 days'),
    ('marcus@vancerobotics.com', 'contacted', 'qualified', now() - interval '15 days'),
    ('marcus@vancerobotics.com', 'qualified', 'proposal_sent', now() - interval '8 days'),
    ('marcus@vancerobotics.com', 'proposal_sent', 'negotiation', now() - interval '2 days'),

    ('tara@acuitybiolabs.com', NULL, 'new', now() - interval '22 days'),
    ('tara@acuitybiolabs.com', 'new', 'contacted', now() - interval '19 days'),
    ('tara@acuitybiolabs.com', 'contacted', 'qualified', now() - interval '13 days'),
    ('tara@acuitybiolabs.com', 'qualified', 'proposal_sent', now() - interval '7 days'),
    ('tara@acuitybiolabs.com', 'proposal_sent', 'negotiation', now() - interval '3 days'),

    ('daniel@northwindstudios.com', NULL, 'new', now() - interval '24 days'),
    ('daniel@northwindstudios.com', 'new', 'contacted', now() - interval '20 days'),
    ('daniel@northwindstudios.com', 'contacted', 'qualified', now() - interval '14 days'),
    ('daniel@northwindstudios.com', 'qualified', 'proposal_sent', now() - interval '6 days'),

    ('sunita@evergreensolar.in', NULL, 'new', now() - interval '20 days'),
    ('sunita@evergreensolar.in', 'new', 'contacted', now() - interval '17 days'),
    ('sunita@evergreensolar.in', 'contacted', 'qualified', now() - interval '11 days'),
    ('sunita@evergreensolar.in', 'qualified', 'proposal_sent', now() - interval '4 days'),

    ('david.chen@pacificwave.hk', NULL, 'new', now() - interval '18 days'),
    ('david.chen@pacificwave.hk', 'new', 'contacted', now() - interval '15 days'),
    ('david.chen@pacificwave.hk', 'contacted', 'qualified', now() - interval '9 days'),
    ('david.chen@pacificwave.hk', 'qualified', 'proposal_sent', now() - interval '3 days'),

    ('aarti@voguesphere.com', NULL, 'new', now() - interval '16 days'),
    ('aarti@voguesphere.com', 'new', 'contacted', now() - interval '13 days'),
    ('aarti@voguesphere.com', 'contacted', 'qualified', now() - interval '8 days'),
    ('aarti@voguesphere.com', 'qualified', 'proposal_sent', now() - interval '5 days'),

    ('meera.iyer@sunloop.energy', NULL, 'new', now() - interval '21 days'),
    ('meera.iyer@sunloop.energy', 'new', 'contacted', now() - interval '16 days'),
    ('meera.iyer@sunloop.energy', 'contacted', 'qualified', now() - interval '7 days'),

    ('tom.w@ledgerline.com', NULL, 'new', now() - interval '19 days'),
    ('tom.w@ledgerline.com', 'new', 'contacted', now() - interval '14 days'),
    ('tom.w@ledgerline.com', 'contacted', 'qualified', now() - interval '5 days'),

    ('neha@healthbridgeai.com', NULL, 'new', now() - interval '15 days'),
    ('neha@healthbridgeai.com', 'new', 'contacted', now() - interval '11 days'),
    ('neha@healthbridgeai.com', 'contacted', 'qualified', now() - interval '4 days'),

    ('rajesh@bharatlogistics.in', NULL, 'new', now() - interval '14 days'),
    ('rajesh@bharatlogistics.in', 'new', 'contacted', now() - interval '10 days'),
    ('rajesh@bharatlogistics.in', 'contacted', 'qualified', now() - interval '4 days'),

    ('sneha@arcadiainteriors.in', NULL, 'new', now() - interval '12 days'),
    ('sneha@arcadiainteriors.in', 'new', 'contacted', now() - interval '6 days'),

    ('arjun.bhatia@peakforge.dev', NULL, 'new', now() - interval '10 days'),
    ('arjun.bhatia@peakforge.dev', 'new', 'contacted', now() - interval '4 days'),

    ('zayn@gulfstream.ae', NULL, 'new', now() - interval '9 days'),
    ('zayn@gulfstream.ae', 'new', 'contacted', now() - interval '3 days'),

    ('grace.lin@halcyonhealth.io', NULL, 'new', now() - interval '5 days'),
    ('imran@trailheadvc.com', NULL, 'new', now() - interval '3 days'),
    ('nikita.rao@fablane.shop', NULL, 'new', now() - interval '2 days'),
    ('samuel@quaysidemedia.tv', NULL, 'new', now() - interval '18 hours')
) AS h(email, from_stage, to_stage, changed_at)
  ON lower(l.email) = h.email;

-- 5. Rich Multi-Touch Activities (Calls, Meetings, Emails, Notes)
INSERT INTO activities (lead_id, type, note, created_at)
SELECT l.id, a.type::activity_type, a.note, a.created_at
FROM leads l
JOIN (VALUES
    -- Vertex Analytics
    ('ananya@vertexanalytics.io', 'created', 'Inbound lead captured via pricing page calculator.', now() - interval '55 days'),
    ('ananya@vertexanalytics.io', 'call', 'Discovery call: 45 user licenses, needs custom Salesforce migration.', now() - interval '52 days'),
    ('ananya@vertexanalytics.io', 'meeting', 'Technical deep-dive on REST API endpoints and webhooks.', now() - interval '46 days'),
    ('ananya@vertexanalytics.io', 'email', 'Shared enterprise security questionnaire and architecture specs.', now() - interval '40 days'),
    ('ananya@vertexanalytics.io', 'call', 'Walked through proposal terms with VP Engineering.', now() - interval '32 days'),
    ('ananya@vertexanalytics.io', 'meeting', 'Final contract alignment with CFO and legal counsel.', now() - interval '15 days'),
    ('ananya@vertexanalytics.io', 'note', 'Master Services Agreement executed. Annual payment received.', now() - interval '5 days'),

    -- Blue Harbour Logistics
    ('rohan.mehta@blueharbour.co', 'created', 'Referred by Ananya from Vertex Analytics.', now() - interval '48 days'),
    ('rohan.mehta@blueharbour.co', 'call', 'Qualification call: Multi-warehouse logistics tracking workflow.', now() - interval '45 days'),
    ('rohan.mehta@blueharbour.co', 'meeting', 'Executive product demo for operations team.', now() - interval '38 days'),
    ('rohan.mehta@blueharbour.co', 'email', 'Delivered pricing proposal for 25 concurrent dispatchers.', now() - interval '30 days'),
    ('rohan.mehta@blueharbour.co', 'call', 'Clarified SLA support guarantees and response times.', now() - interval '15 days'),
    ('rohan.mehta@blueharbour.co', 'note', 'PO received. Fast close, zero friction.', now() - interval '8 days'),

    -- Priya Nair (Kestrel Foods - In Negotiation)
    ('priya.nair@kestrelfoods.com', 'created', 'LinkedIn message inquiry regarding retail FMCG CRM.', now() - interval '28 days'),
    ('priya.nair@kestrelfoods.com', 'call', 'Discovery call: Distributor management and retailer sales tracking.', now() - interval '23 days'),
    ('priya.nair@kestrelfoods.com', 'meeting', 'Interactive product walkthrough with regional sales heads.', now() - interval '17 days'),
    ('priya.nair@kestrelfoods.com', 'email', 'Sent formal quote and multi-year volume discount schedule.', now() - interval '11 days'),
    ('priya.nair@kestrelfoods.com', 'call', 'Commercial negotiation call. Requested 10% discount on year 2.', now() - interval '2 days'),
    ('priya.nair@kestrelfoods.com', 'meeting', 'Scheduled final contract sign-off call for tomorrow 3 PM.', now() - interval '1 days'),

    -- Marcus Vance (Vance Robotics - In Negotiation)
    ('marcus@vancerobotics.com', 'created', 'Introduced via partner advisory board.', now() - interval '25 days'),
    ('marcus@vancerobotics.com', 'call', 'High-level scoping for 60 global engineering sales leads.', now() - interval '20 days'),
    ('marcus@vancerobotics.com', 'meeting', 'Product demonstration highlighting lead scoring matrix.', now() - interval '14 days'),
    ('marcus@vancerobotics.com', 'email', 'Sent revised MSA with agreed payment milestones.', now() - interval '5 days'),
    ('marcus@vancerobotics.com', 'call', 'CEO verbal approval confirmed. Legal redlining in progress.', now() - interval '2 days'),

    -- Daniel Okoro (Northwind Studios)
    ('daniel@northwindstudios.com', 'created', 'Organic website inquiry from creative agency directory.', now() - interval '24 days'),
    ('daniel@northwindstudios.com', 'call', 'Initial scoping: client project onboarding workflows.', now() - interval '19 days'),
    ('daniel@northwindstudios.com', 'meeting', 'Demonstrated workflow automation rules and email triggers.', now() - interval '12 days'),
    ('daniel@northwindstudios.com', 'email', 'Issued updated proposal v2 with custom agency onboarding.', now() - interval '3 days'),

    -- Sunita Agarwal (Evergreen Solar)
    ('sunita@evergreensolar.in', 'created', 'Outbound phone outreach to commercial solar installers.', now() - interval '20 days'),
    ('sunita@evergreensolar.in', 'call', 'Discussed field agent mobile access and lead geolocation.', now() - interval '16 days'),
    ('sunita@evergreensolar.in', 'meeting', 'Demo conducted for territory managers.', now() - interval '10 days'),
    ('sunita@evergreensolar.in', 'email', 'Shared comprehensive implementation timeline and quote.', now() - interval '4 days'),

    -- David Chen (Pacific Wave)
    ('david.chen@pacificwave.hk', 'created', 'Referred by Singapore trade association.', now() - interval '18 days'),
    ('david.chen@pacificwave.hk', 'call', 'Scoping international import pipeline stages.', now() - interval '14 days'),
    ('david.chen@pacificwave.hk', 'meeting', 'Demonstrated custom stage pipeline board and currencies.', now() - interval '8 days'),
    ('david.chen@pacificwave.hk', 'email', 'Delivered commercial proposal with wire transfer terms.', now() - interval '2 days'),

    -- Meera Iyer (Sunloop Energy - Qualified)
    ('meera.iyer@sunloop.energy', 'created', 'Targeted enterprise cold call campaign.', now() - interval '21 days'),
    ('meera.iyer@sunloop.energy', 'call', 'Detailed BANT qualification: Budget allocated, Meera is champion.', now() - interval '15 days'),
    ('meera.iyer@sunloop.energy', 'meeting', 'Technical demonstration with Head of Sales Operations.', now() - interval '7 days'),

    -- Tom Whitfield (Ledgerline Fintech - Qualified)
    ('tom.w@ledgerline.com', 'created', 'Referred by seed investor network.', now() - interval '19 days'),
    ('tom.w@ledgerline.com', 'call', 'Reviewed security requirements, multi-factor auth and audit logs.', now() - interval '13 days'),
    ('tom.w@ledgerline.com', 'email', 'Forwarded SOC2 compliance bundle and security whitepaper.', now() - interval '6 days'),

    -- Sneha Kulkarni (Arcadia Interiors - Contacted)
    ('sneha@arcadiainteriors.in', 'created', 'Lead generated from LinkedIn Sponsored Showcase.', now() - interval '12 days'),
    ('sneha@arcadiainteriors.in', 'call', 'Introductory conversation. Wants lead capture from Instagram ads.', now() - interval '5 days'),

    -- Arjun Bhatia (Peakforge Tooling - Contacted)
    ('arjun.bhatia@peakforge.dev', 'created', 'Downloaded Developer API documentation from website.', now() - interval '10 days'),
    ('arjun.bhatia@peakforge.dev', 'email', 'Sent quick onboarding link and invited to product overview.', now() - interval '4 days'),

    -- Fresh Inbounds
    ('grace.lin@halcyonhealth.io', 'created', 'Inbound referral from regional health tech consortium.', now() - interval '5 days'),
    ('grace.lin@halcyonhealth.io', 'note', 'Assigned to Rohan Sharma. Priority enterprise account.', now() - interval '4 days'),
    ('imran@trailheadvc.com', 'created', 'LinkedIn outreach response.', now() - interval '3 days'),
    ('nikita.rao@fablane.shop', 'created', 'Website contact form submission.', now() - interval '2 days'),
    ('samuel@quaysidemedia.tv', 'created', 'Inbound website request.', now() - interval '18 hours')
) AS a(email, type, note, created_at)
  ON lower(l.email) = a.email;

-- 6. Open & Strategic Follow-up Tasks
INSERT INTO tasks (lead_id, title, due_date, type, notes, assignee_id, completed_at, created_at)
SELECT l.id, t.title, t.due_date, t.type, t.notes, tm.id, t.completed_at::timestamptz, t.created_at
FROM leads l
JOIN (VALUES
    -- Priya Nair: Final Contract Signing (Due today/tomorrow)
    ('priya.nair@kestrelfoods.com', 'Contract finalization & closing review call', current_date + 1, 'call', 'Confirm 2-year pricing agreement and send DocuSign.', 'ananya.sen@pipelinecrm.io', NULL, now() - interval '2 days'),
    -- Marcus Vance: Board sign-off
    ('marcus@vancerobotics.com', 'Follow-up with Marcus on C-level approval', current_date + 2, 'meeting', 'Review redlined payment milestone schedule.', 'rohan.sharma@pipelinecrm.io', NULL, now() - interval '3 days'),
    -- Tara Deshmukh: Procurement check
    ('tara@acuitybiolabs.com', 'Procurement vendor registration follow-up', current_date, 'call', 'Verify if GST and bank details received by finance team.', 'kavita.rao@pipelinecrm.io', NULL, now() - interval '4 days'),
    -- Daniel Okoro: Proposal chasing
    ('daniel@northwindstudios.com', 'Proposal v2 review check-in', current_date + 3, 'email', 'Check if creative directors reviewed scope v2.', 'ananya.sen@pipelinecrm.io', NULL, now() - interval '5 days'),
    -- Sunita Agarwal: Send demo recording
    ('sunita@evergreensolar.in', 'Send mobile app recording to Sunita', current_date - 1, 'follow_up', 'Overdue follow-up for field sales app demo.', 'vikram.patel@pipelinecrm.io', NULL, now() - interval '6 days'),
    -- David Chen: Currency quote
    ('david.chen@pacificwave.hk', 'Review USD pricing terms with David', current_date + 4, 'call', 'Discuss Asian Pacific deployment schedule.', 'rohan.sharma@pipelinecrm.io', NULL, now() - interval '2 days'),
    -- Tom Whitfield: Security review
    ('tom.w@ledgerline.com', 'Schedule meeting with Tom and InfoSec Lead', current_date + 5, 'meeting', 'Address encryption and role-based access control.', 'rohan.sharma@pipelinecrm.io', NULL, now() - interval '3 days'),
    -- Grace Lin: Discovery call
    ('grace.lin@halcyonhealth.io', 'Introductory discovery call with Grace Lin', current_date + 1, 'call', 'High-priority inbound referral.', 'rohan.sharma@pipelinecrm.io', NULL, now() - interval '2 days'),
    -- Arjun Bhatia: Tooling demo
    ('arjun.bhatia@peakforge.dev', 'Technical demo of webhook integration', current_date + 2, 'demo', 'Prepare developer sample payload.', 'rohan.sharma@pipelinecrm.io', NULL, now() - interval '1 days')
) AS t(email, title, due_date, type, notes, assignee_email, completed_at, created_at)
  ON lower(l.email) = t.email
LEFT JOIN team_members tm ON lower(tm.email) = t.assignee_email;

COMMIT;
