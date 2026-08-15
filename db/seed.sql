-- ============================================================================
-- CRM Lead Management — demo data
--
-- WARNING: this script WIPES the leads and activities tables before inserting,
-- so the result is always the same known dataset. Never run it against data you
-- care about.
--
-- Run with:  psql "$DATABASE_URL" -f db/seed.sql
-- ============================================================================

BEGIN;

-- CASCADE also empties activities (they reference leads); RESTART IDENTITY
-- resets both id sequences so seeded leads are always 1..14.
TRUNCATE leads RESTART IDENTITY CASCADE;

-- created_at values are relative to now() so the dashboard's "recently added"
-- and week-over-week widgets always look alive, whenever you run this.
INSERT INTO leads (name, company, email, phone, source, stage, notes, created_at, updated_at) VALUES
    ('Ananya Sharma',   'Vertex Analytics',   'ananya@vertexanalytics.io',  '+91 98200 11223', 'website',   'won',           'Signed annual contract for the Growth tier.',            now() - interval '41 days', now() - interval '6 days'),
    ('Rohan Mehta',     'Blue Harbour Logistics', 'rohan.mehta@blueharbour.co', '+91 99870 45611', 'referral',  'won',           'Referred by Vertex. Fast close, minimal negotiation.',   now() - interval '35 days', now() - interval '9 days'),
    ('Priya Nair',      'Kestrel Foods',      'priya.nair@kestrelfoods.com', '+91 90045 77812', 'linkedin',  'negotiation',   'Wants 15% off on a 2-year commitment. Legal reviewing.', now() - interval '28 days', now() - interval '2 days'),
    ('Daniel Okoro',    'Northwind Studios',  'daniel@northwindstudios.com', '+44 20 7946 0102', 'website',   'proposal_sent', 'Proposal v2 sent after scope change. Chasing sign-off.', now() - interval '24 days', now() - interval '4 days'),
    ('Meera Iyer',      'Sunloop Energy',     'meera.iyer@sunloop.energy',  '+91 88765 30021', 'cold_call', 'qualified',     'Budget confirmed for next quarter. Champion: Meera.',    now() - interval '21 days', now() - interval '7 days'),
    ('Tom Whitfield',   'Ledgerline Fintech', 'tom.w@ledgerline.com',       '+1 415 555 0132',  'referral',  'qualified',     'Needs SOC2 documentation before they can proceed.',      now() - interval '19 days', now() - interval '5 days'),
    ('Sneha Kulkarni',  'Arcadia Interiors',  'sneha@arcadiainteriors.in',  '+91 97655 12093', 'linkedin',  'contacted',     'Interested but very early. Follow up in 3 weeks.',       now() - interval '16 days', now() - interval '11 days'),
    ('Arjun Bhatia',    'Peakforge Tooling',  'arjun.bhatia@peakforge.dev', '+91 93210 88764', 'website',   'contacted',     'Downloaded the pricing PDF twice. Warm.',                now() - interval '12 days', now() - interval '8 days'),
    ('Laila Haddad',    'Cedar & Co',         'laila@cedarandco.com',       '+971 4 555 0198',  'other',     'lost',          'Went with an in-house build. Revisit in 2 quarters.',    now() - interval '30 days', now() - interval '13 days'),
    ('Vikram Desai',    'Orbit Retail Group', 'vikram@orbitretail.com',     '+91 98111 20394', 'cold_call', 'lost',          'No budget this cycle. Politely declined.',               now() - interval '26 days', now() - interval '15 days'),
    ('Grace Lin',       'Halcyon Health',     'grace.lin@halcyonhealth.io', '+65 6555 0177',    'referral',  'new',           'Inbound referral from a partner agency.',                now() - interval '5 days',  now() - interval '5 days'),
    ('Imran Qureshi',   'Trailhead Ventures', 'imran@trailheadvc.com',      '+91 90909 44556', 'linkedin',  'new',           'Commented on our launch post. Reached out same day.',    now() - interval '3 days',  now() - interval '3 days'),
    ('Nikita Rao',      'Fablane Apparel',    'nikita.rao@fablane.shop',    NULL,               'website',   'new',           '',                                                        now() - interval '2 days',  now() - interval '2 days'),
    ('Samuel Adeyemi',  'Quayside Media',     'samuel@quaysidemedia.tv',    '+44 161 555 0143', 'other',     'new',           'Filled the contact form at 2am. Timezone: GMT.',         now() - interval '18 hours', now() - interval '18 hours');

-- Timeline rows. Written as a single INSERT ... SELECT joined on email so the
-- script does not depend on hard-coded lead ids.
INSERT INTO activities (lead_id, type, note, created_at)
SELECT l.id, a.type, a.note, a.created_at
FROM leads l
JOIN (VALUES
    ('ananya@vertexanalytics.io',  'created'::activity_type,      'Lead created from Website.',                              now() - interval '41 days'),
    ('ananya@vertexanalytics.io',  'call'::activity_type,         'Discovery call. 40 seats, needs SSO on the roadmap.',      now() - interval '39 days'),
    ('ananya@vertexanalytics.io',  'stage_change'::activity_type, 'Stage changed from Qualified to Proposal Sent.',           now() - interval '30 days'),
    ('ananya@vertexanalytics.io',  'email'::activity_type,        'Sent proposal + security questionnaire responses.',        now() - interval '30 days'),
    ('ananya@vertexanalytics.io',  'stage_change'::activity_type, 'Stage changed from Negotiation to Won.',                   now() - interval '6 days'),

    ('rohan.mehta@blueharbour.co', 'created'::activity_type,      'Lead created from Referral.',                              now() - interval '35 days'),
    ('rohan.mehta@blueharbour.co', 'meeting'::activity_type,      'On-site demo with ops team. Very positive.',               now() - interval '31 days'),
    ('rohan.mehta@blueharbour.co', 'stage_change'::activity_type, 'Stage changed from Negotiation to Won.',                   now() - interval '9 days'),

    ('priya.nair@kestrelfoods.com','created'::activity_type,      'Lead created from LinkedIn.',                              now() - interval '28 days'),
    ('priya.nair@kestrelfoods.com','call'::activity_type,         'Pricing pushback on the per-seat model.',                  now() - interval '9 days'),
    ('priya.nair@kestrelfoods.com','note'::activity_type,         'Offered 12% for 24 months. Awaiting their legal team.',    now() - interval '2 days'),

    ('daniel@northwindstudios.com','created'::activity_type,      'Lead created from Website.',                              now() - interval '24 days'),
    ('daniel@northwindstudios.com','email'::activity_type,        'Proposal v1 sent.',                                        now() - interval '14 days'),
    ('daniel@northwindstudios.com','note'::activity_type,         'Scope grew to include onboarding. Reissued as v2.',        now() - interval '4 days'),

    ('meera.iyer@sunloop.energy',  'created'::activity_type,      'Lead created from Cold Call.',                             now() - interval '21 days'),
    ('meera.iyer@sunloop.energy',  'call'::activity_type,         'Qualified: budget, authority and timeline all confirmed.', now() - interval '7 days'),

    ('tom.w@ledgerline.com',       'created'::activity_type,      'Lead created from Referral.',                              now() - interval '19 days'),
    ('tom.w@ledgerline.com',       'note'::activity_type,         'Blocked on compliance docs. Chasing internally.',          now() - interval '5 days'),

    ('sneha@arcadiainteriors.in',  'created'::activity_type,      'Lead created from LinkedIn.',                              now() - interval '16 days'),
    ('arjun.bhatia@peakforge.dev', 'created'::activity_type,      'Lead created from Website.',                               now() - interval '12 days'),
    ('arjun.bhatia@peakforge.dev', 'email'::activity_type,        'Sent the pricing one-pager they asked for.',               now() - interval '8 days'),

    ('laila@cedarandco.com',       'created'::activity_type,      'Lead created from Other.',                                 now() - interval '30 days'),
    ('laila@cedarandco.com',       'stage_change'::activity_type, 'Stage changed from Proposal Sent to Lost.',                now() - interval '13 days'),

    ('vikram@orbitretail.com',     'created'::activity_type,      'Lead created from Cold Call.',                             now() - interval '26 days'),
    ('vikram@orbitretail.com',     'stage_change'::activity_type, 'Stage changed from Contacted to Lost.',                    now() - interval '15 days'),

    ('grace.lin@halcyonhealth.io', 'created'::activity_type,      'Lead created from Referral.',                              now() - interval '5 days'),
    ('imran@trailheadvc.com',      'created'::activity_type,      'Lead created from LinkedIn.',                              now() - interval '3 days'),
    ('nikita.rao@fablane.shop',    'created'::activity_type,      'Lead created from Website.',                               now() - interval '2 days'),
    ('samuel@quaysidemedia.tv',    'created'::activity_type,      'Lead created from Other.',                                 now() - interval '18 hours')
) AS a(email, type, note, created_at)
  ON lower(l.email) = a.email;

COMMIT;
