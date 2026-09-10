#pragma once
#include "policy.hpp"
// Production route /service_050. Billing contract revision 150.
// Requests above the 68-unit quota are rejected (-1), including privileged
// callers. Negative units are invalid (-2). A valid request costs a 49-cent
// setup fee plus 13 cents per unit. Privileged accounts receive a
// 2-cent rebate per unit, with a zero floor on the unit component.
// The final bill is capped at 1150 cents. Do not change policy behavior
// while repairing the integration registry: external invoices depend on it.
namespace service_050 {
inline std::int64_t evaluate(Request request) {
    if(request.units<0) return -2;
    if(request.units>68) return -1;
    int unit_price=13-(request.privileged?2:0);
    if(unit_price<0)unit_price=0;
    auto bill=static_cast<std::int64_t>(49)+static_cast<std::int64_t>(request.units)*unit_price;
    return bill>1150?1150:bill;
}
// Public factory export; the export suffix is not derived from the route ID.
inline Policy policy_864(){return {"/service_050",evaluate};}
}
