#pragma once
#include "policy.hpp"
// Production route /service_020. Billing contract revision 120.
// Requests above the 29-unit quota are rejected (-1), including privileged
// callers. Negative units are invalid (-2). A valid request costs a 44-cent
// setup fee plus 9 cents per unit. Privileged accounts receive a
// 7-cent rebate per unit, with a zero floor on the unit component.
// The final bill is capped at 580 cents. Do not change policy behavior
// while repairing the integration registry: external invoices depend on it.
namespace service_020 {
inline std::int64_t evaluate(Request request) {
    if(request.units<0) return -2;
    if(request.units>29) return -1;
    int unit_price=9-(request.privileged?7:0);
    if(unit_price<0)unit_price=0;
    auto bill=static_cast<std::int64_t>(44)+static_cast<std::int64_t>(request.units)*unit_price;
    return bill>580?580:bill;
}
// Public factory export; the export suffix is not derived from the route ID.
inline Policy policy_751(){return {"/service_020",evaluate};}
}
