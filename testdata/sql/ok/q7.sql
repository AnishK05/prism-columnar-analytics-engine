-- Q7: string IN-list
SELECT COUNT(*) FROM events WHERE event_type IN ('purchase', 'refund');
